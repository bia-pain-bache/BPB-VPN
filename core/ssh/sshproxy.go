// Package sshproxy runs an SSH session and exposes it as a local SOCKS5 proxy.
// It is owned by the VPN controller and runs in-process; it is not a daemon.
package sshproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"
	"time"
)

type config struct {
	Server, User, Password, PrivateKey, Passphrase, HostKey, Upstream, Listen string
	KeepAliveInterval, TCPKeepAliveInterval                                   int
}

// A variable so tests can listen on a free port.
var socksListenAddress = "127.0.0.1:1080"

// firstConnectWait is how long Start waits for the first connection, or for a permanent failure
// (wrong credentials, changed host key), before returning.
var firstConnectWait = 10 * time.Second

// Config is the SSH settings passed from the app to the VPN core.
type Config struct {
	Host                 string `json:"host"`
	Port                 int    `json:"port"`
	User                 string `json:"user"`
	Password             string `json:"password"`
	PrivateKey           string `json:"privateKey"`
	Passphrase           string `json:"passphrase"`
	KeepAliveInterval    int    `json:"keepAliveInterval"`
	TCPKeepAliveInterval int    `json:"tcpKeepAliveInterval"`
	// HostKey is the server's key (SHA256:…) the app remembered from an earlier connection. Empty
	// trusts the first key seen; Server.HostKey returns it for the app to remember.
	HostKey string `json:"hostKey"`
}

// Server owns the SSH connection, SOCKS listener, and reconnect loop.
type Server struct {
	listener net.Listener
	session  *session
	cancel   context.CancelFunc
	done     chan struct{}
	once     sync.Once
}

// Start opens the local SOCKS listener and establishes the first SSH session.
func Start(c Config) (*Server, error) {
	if c.Host == "" || c.User == "" || c.Port < 1 || c.Port > 65535 {
		return nil, errors.New("the host, port and username are required")
	}
	if c.Password == "" && c.PrivateKey == "" {
		return nil, errors.New("a password or private key is required")
	}
	server := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	socks, err := net.Listen("tcp", socksListenAddress)
	if err != nil {
		log.Printf("SSH SOCKS listen on %s: %v", socksListenAddress, err)
		return nil, fmt.Errorf("can't listen on %s: another app may be using that port", socksListenAddress)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s, err := newSession(config{
		Server: server, User: c.User, Password: c.Password,
		PrivateKey: c.PrivateKey, Passphrase: c.Passphrase, HostKey: c.HostKey,
		KeepAliveInterval:    c.KeepAliveInterval,
		TCPKeepAliveInterval: c.TCPKeepAliveInterval,
	})
	if err != nil {
		cancel()
		socks.Close()
		return nil, err
	}
	s.ctx = ctx

	// Connect in the background. The SOCKS listener is already open; dials through the session
	// wait on its ready channel until the first connection is established or the session fails
	// permanently. The first connection is retried with backoff until Stop.
	s.mu.Lock()
	ready := s.ready
	s.mu.Unlock()
	go func() {
		if err := s.connectFirstContext(ctx); err != nil {
			log.Printf("SSH initial connection to %s: %v", s.cfg.Server, err)
			var auth errAuth
			if errors.As(err, &auth) {
				s.fatal(err)
			}
		}
	}()

	proxy := &Server{listener: socks, session: s, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(proxy.done)
		if err := serveSocks(socks, s); err != nil && ctx.Err() == nil {
			log.Printf("SSH SOCKS listener stopped: %v", err)
		}
	}()

	// A permanent failure (wrong password or key, changed host key) is returned now, so it reaches
	// the app like a settings error. A slow or unreachable server is not an error here: the
	// session keeps retrying in the background.
	timer := time.NewTimer(firstConnectWait)
	defer timer.Stop()
	select {
	case <-ready:
	case <-s.dead:
		err := s.err()
		proxy.Stop()
		return nil, err
	case <-timer.C:
	}
	return proxy, nil
}

// StartJSON validates a JSON settings object and starts the SOCKS service.
func StartJSON(configJSON string) (*Server, error) {
	var c Config
	if err := json.Unmarshal([]byte(configJSON), &c); err != nil {
		return nil, fmt.Errorf("SSH settings: %w", err)
	}
	return Start(c)
}

// HostKey is the server's key (SHA256:…), as pinned in Config or learned on the first
// connection; "" before then, and for a nil Server.
func (s *Server) HostKey() string {
	if s == nil {
		return ""
	}
	return s.session.hostKey()
}

// Stop closes the listener and SSH session. It is safe to call more than once.
func (s *Server) Stop() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		s.cancel()
		s.listener.Close()
		s.session.close()
		<-s.done
	})
}