package sshproxy

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/net/proxy"
)

const (
	dialTimeout = 20 * time.Second
	maxBackoff  = 30 * time.Second
)

func newSession(c config) (*session, error) {
	s := &session{cfg: c, ready: make(chan struct{}), ctx: context.Background()}
	return s, s.initialize()
}

// session keeps one SSH client connected, reconnecting when it drops.
type session struct {
	cfg    config
	ssh    *ssh.ClientConfig
	dialer proxy.Dialer
	ctx    context.Context

	mu     sync.Mutex
	client *ssh.Client
	// ready is closed while a client is connected and replaced when it drops.
	ready chan struct{}
	lost  bool
}

// errAuth: wrong credentials or host key; retrying won't help.
type errAuth struct{ error }

func (s *session) initialize() error {
	c := s.cfg
	var auth []ssh.AuthMethod
	if c.PrivateKey != "" {
		signer, err := parseKey(c.PrivateKey, c.Passphrase)
		if err != nil {
			return err
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if c.Password != "" {
		auth = append(auth,
			ssh.Password(c.Password),
			// Many servers ask for the password as keyboard-interactive.
			ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = c.Password
				}
				return answers, nil
			}))
	}

	keepAlive := time.Duration(c.TCPKeepAliveInterval) * time.Second
	var dialer proxy.Dialer = &net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: keepAlive,
	}
	if c.Upstream != "" {
		d, err := proxy.SOCKS5("tcp", c.Upstream, nil, dialer)
		if err != nil {
			return fmt.Errorf("upstream: %v", err)
		}
		dialer = d
	}

	s.dialer = dialer
	s.ssh = &ssh.ClientConfig{
		User:            c.User,
		Auth:            auth,
		HostKeyCallback: s.checkHostKey,
		Timeout:         dialTimeout,
	}
	return nil
}

// parseKey reads a PEM / OpenSSH private key, decrypting it with passphrase if it's encrypted.
func parseKey(key, passphrase string) (ssh.Signer, error) {
	var signer ssh.Signer
	var err error
	if passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(key), []byte(passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey([]byte(key))
	}
	var missing *ssh.PassphraseMissingError
	switch {
	case errors.As(err, &missing):
		return nil, errors.New("the private key is encrypted: set its passphrase")
	case errors.Is(err, x509.IncorrectPasswordError) || err != nil && strings.Contains(err.Error(), "decryption password incorrect"):
		return nil, errors.New("wrong passphrase for the private key")
	case err != nil:
		return nil, fmt.Errorf("private key: %v", err)
	}
	return signer, nil
}

// checkHostKey accepts the pinned key, or any key when none is pinned (reporting it so the
// app pins it).
func (s *session) checkHostKey(_ string, _ net.Addr, key ssh.PublicKey) error {
	got := ssh.FingerprintSHA256(key)
	if s.cfg.HostKey == "" {
		s.cfg.HostKey = got
		log.Printf("SSH host key: %s", got)
		return nil
	}
	if got != s.cfg.HostKey {
		return errAuth{fmt.Errorf("the server's host key changed: expected %s, got %s", s.cfg.HostKey, got)}
	}
	return nil
}

func (s *session) connect() (*ssh.Client, error) {
	conn, err := s.dialer.Dial("tcp", s.cfg.Server)
	if err != nil {
		return nil, err
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, s.cfg.Server, s.ssh)
	if err != nil {
		conn.Close()
		var hostKey errAuth
		if errors.As(err, &hostKey) || strings.Contains(err.Error(), "unable to authenticate") {
			return nil, errAuth{err}
		}
		return nil, err
	}
	return ssh.NewClient(c, chans, reqs), nil
}

// connectFirst connects, retrying network failures until it works; wrong credentials or a
// changed host key end the process.
func (s *session) connectFirst() error {
	return s.connectFirstContext(s.ctx)
}

func (s *session) connectFirstContext(ctx context.Context) error {
	for backoff := time.Second; ; backoff = min(backoff*2, maxBackoff) {
		if err := ctx.Err(); err != nil {
			return err
		}
		c, err := s.connect()
		if err == nil {
			log.Printf("connected to %s", s.cfg.Server)
			s.use(c)
			return nil
		}
		var auth errAuth
		if errors.As(err, &auth) {
			return err
		}
		log.Printf("connecting to %s: %v (retrying in %s)", s.cfg.Server, err, backoff)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// use makes c the session's client and watches it.
func (s *session) use(c *ssh.Client) {
	s.mu.Lock()
	s.client = c
	close(s.ready)
	wasLost := s.lost
	s.lost = false
	s.mu.Unlock()
	if wasLost {
		log.Printf("reconnected to %s", s.cfg.Server)
	}
	go s.watch(c)
}

// watch sends keepalives until the client fails, then reconnects.
func (s *session) watch(c *ssh.Client) {
	done := make(chan struct{})
	go func() {
		c.Wait()
		close(done)
	}()
	interval := time.Duration(s.cfg.KeepAliveInterval) * time.Second
	if interval <= 0 {
		interval = 15 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-done:
			s.drop(c)
			return
		case <-t.C:
			if _, _, err := c.SendRequest("keepalive@openssh.com", true, nil); err != nil {
				c.Close()
			}
		}
	}
}

// drop forgets the failed client and reconnects.
func (s *session) drop(c *ssh.Client) {
	s.mu.Lock()
	if s.client != c {
		s.mu.Unlock()
		return
	}
	s.client = nil
	s.ready = make(chan struct{})
	s.lost = true
	s.mu.Unlock()
	log.Printf("SSH session lost; reconnecting")
	for backoff := time.Second; ; backoff = min(backoff*2, maxBackoff) {
		if s.ctx.Err() != nil {
			return
		}
		next, err := s.connect()
		if err == nil {
			log.Printf("reconnected to %s", s.cfg.Server)
			s.use(next)
			return
		}
		var auth errAuth
		if errors.As(err, &auth) {
			log.Printf("SSH reconnect stopped: %v", err)
			return
		}
		log.Printf("reconnecting to %s: %v (retrying in %s)", s.cfg.Server, err, backoff)
		timer := time.NewTimer(backoff)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (s *session) close() {
	s.mu.Lock()
	c := s.client
	s.client = nil
	s.mu.Unlock()
	if c != nil {
		_ = c.Close()
	}
}

// dial opens a direct-tcpip channel to addr ("host:port"; the server resolves names), waiting
// for a reconnect when the session is down.
func (s *session) dial(addr string) (net.Conn, error) {
	s.mu.Lock()
	c, ready := s.client, s.ready
	s.mu.Unlock()
	if c == nil {
		select {
		case <-ready:
		case <-time.After(dialTimeout):
			return nil, errors.New("SSH session is reconnecting")
		}
		s.mu.Lock()
		c = s.client
		s.mu.Unlock()
		if c == nil {
			return nil, errors.New("SSH session is reconnecting")
		}
	}
	return c.Dial("tcp", addr)
}
