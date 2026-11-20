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
	// keepaliveTimeout: how long a keepalive may go unanswered before the session counts as dead.
	keepaliveTimeout = 10 * time.Second
)

func newSession(c config) (*session, error) {
	s := &session{
		cfg: c, ready: make(chan struct{}), dead: make(chan struct{}), ctx: context.Background(),
		dialTimeout: dialTimeout, keepaliveTimeout: keepaliveTimeout, handshakeTimeout: handshakeTimeout,
	}
	return s, s.initialize()
}

// session keeps one SSH client connected, reconnecting when it drops.
type session struct {
	cfg    config
	ssh    *ssh.ClientConfig
	dialer proxy.Dialer
	ctx    context.Context
	// The package defaults; fields so a test can shorten them for one session.
	dialTimeout, keepaliveTimeout, handshakeTimeout time.Duration

	mu     sync.Mutex
	client *ssh.Client
	// ready is closed while a client is connected and replaced when it drops.
	ready chan struct{}
	lost  bool
	// dead is closed, and deadErr set, when the session ends for good: wrong credentials or a
	// changed host key on a reconnect, where retrying won't help.
	dead    chan struct{}
	deadErr error
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
		// Timeout only bounds ssh.Dial's TCP connect, which isn't used here; connectContext
		// bounds the dial and the handshake itself.
		Timeout: dialTimeout,
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
	s.mu.Lock()
	pinned := s.cfg.HostKey
	if pinned == "" {
		s.cfg.HostKey = got
	}
	s.mu.Unlock()
	if pinned == "" {
		log.Printf("SSH host key: %s", got)
		return nil
	}
	if got != pinned {
		return errAuth{fmt.Errorf("the server's host key changed: expected %s, got %s", pinned, got)}
	}
	return nil
}

// dialContext dials addr through d, honoring ctx where d supports it (net.Dialer and the
// SOCKS5 dialer do, including the SOCKS handshake).
func dialContext(ctx context.Context, d proxy.Dialer, addr string) (net.Conn, error) {
	if cd, ok := d.(proxy.ContextDialer); ok {
		return cd.DialContext(ctx, "tcp", addr)
	}
	return d.Dial("tcp", addr)
}

func (s *session) connect() (*ssh.Client, error) {
	return s.connectContext(s.ctx)
}

// connectContext dials and handshakes, giving up when ctx ends or after dialTimeout.
func (s *session) connectContext(ctx context.Context) (*ssh.Client, error) {
	dctx, cancel := context.WithTimeout(ctx, s.dialTimeout)
	defer cancel()
	conn, err := dialContext(dctx, s.dialer, s.cfg.Server)
	if err != nil {
		return nil, err
	}
	// Bound the handshake, and abort it when ctx ends. A peer that accepts the connection and
	// then says nothing would otherwise block NewClientConn forever.
	conn.SetDeadline(time.Now().Add(s.dialTimeout))
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	c, chans, reqs, err := ssh.NewClientConn(conn, s.cfg.Server, s.ssh)
	stop()
	if err != nil {
		conn.Close()
		var hostKey errAuth
		if errors.As(err, &hostKey) || strings.Contains(err.Error(), "unable to authenticate") {
			return nil, errAuth{err}
		}
		return nil, err
	}
	conn.SetDeadline(time.Time{})
	return ssh.NewClient(c, chans, reqs), nil
}

// connectFirst connects, retrying network failures until it works; wrong credentials or a
// changed host key end the process.
func (s *session) connectFirst() error {
	return s.connectFirstContext(s.ctx)
}

func (s *session) connectFirstContext(ctx context.Context) error {
	var last error
	for backoff := time.Second; ; backoff = min(backoff*2, maxBackoff) {
		if ctx.Err() != nil {
			return s.connectFailure(ctx, last)
		}
		c, err := s.connectContext(ctx)
		if err == nil {
			log.Printf("connected to %s", s.cfg.Server)
			s.use(c)
			return nil
		}
		var auth errAuth
		if errors.As(err, &auth) {
			return err
		}
		if ctx.Err() != nil {
			return s.connectFailure(ctx, last)
		}
		last = err
		log.Printf("connecting to %s: %v (retrying in %s)", s.cfg.Server, err, backoff)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return s.connectFailure(ctx, last)
		case <-timer.C:
		}
	}
}

// connectFailure is the error for a first connection that ctx cut short, with the last
// attempt's error when there was one (a bare "context deadline exceeded" says nothing).
func (s *session) connectFailure(ctx context.Context, last error) error {
	if last != nil {
		return fmt.Errorf("connecting to %s: %w (last error: %v)", s.cfg.Server, ctx.Err(), last)
	}
	return fmt.Errorf("connecting to %s: %w", s.cfg.Server, ctx.Err())
}

// use makes c the session's client and watches it.
func (s *session) use(c *ssh.Client) {
	s.mu.Lock()
	// Checked under the lock, so a client that finishes connecting while the session is being
	// stopped is closed here rather than left running: Stop cancels ctx before it closes the
	// current client.
	if s.ctx.Err() != nil {
		s.mu.Unlock()
		c.Close()
		return
	}
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
		case <-s.ctx.Done():
			c.Close()
			return
		case <-t.C:
			if !alive(c, s.keepaliveTimeout) {
				c.Close() // makes Wait return, so the next loop reconnects
			}
		}
	}
}

// alive sends one keepalive and reports whether it was answered in time. SendRequest has no
// timeout of its own: on a path that silently stops forwarding it blocks until the connection
// closes, so a probe that goes unanswered is abandoned by closing the client.
func alive(c *ssh.Client, timeout time.Duration) bool {
	res := make(chan error, 1)
	go func() {
		_, _, err := c.SendRequest("keepalive@openssh.com", true, nil)
		res <- err
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-res:
		return err == nil
	case <-timer.C:
		return false
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
		next, err := s.connectContext(s.ctx)
		if err == nil {
			log.Printf("reconnected to %s", s.cfg.Server)
			s.use(next)
			return
		}
		if s.ctx.Err() != nil {
			return
		}
		var auth errAuth
		if errors.As(err, &auth) {
			log.Printf("SSH reconnect stopped: %v", err)
			s.fatal(err)
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

// fatal ends the session for good: connections fail at once with err instead of waiting for a
// reconnect that will never come.
func (s *session) fatal(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deadErr != nil || s.ctx.Err() != nil {
		return
	}
	s.deadErr = err
	close(s.dead)
}

// err is why the session ended for good, or nil.
func (s *session) err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deadErr
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
		timer := time.NewTimer(s.dialTimeout)
		defer timer.Stop()
		select {
		case <-ready:
		case <-s.dead:
			return nil, fmt.Errorf("SSH session stopped: %w", s.err())
		case <-s.ctx.Done():
			return nil, errors.New("SSH session is closed")
		case <-timer.C:
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