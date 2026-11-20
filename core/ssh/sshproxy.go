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

const socksListenAddress = "127.0.0.1:1080"

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
		return nil, errors.New("SSH host, valid port and username are required")
	}
	if c.Password == "" && c.PrivateKey == "" {
		return nil, errors.New("SSH password or private key is required")
	}
	server := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	socks, err := net.Listen("tcp", socksListenAddress)
	if err != nil {
		return nil, fmt.Errorf("listen for SSH SOCKS: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	startCtx, startCancel := context.WithTimeout(ctx, time.Minute)
	s, err := newSession(config{
		Server: server, User: c.User, Password: c.Password,
		PrivateKey: c.PrivateKey, Passphrase: c.Passphrase,
		KeepAliveInterval:    c.KeepAliveInterval,
		TCPKeepAliveInterval: c.TCPKeepAliveInterval,
	})
	if err != nil {
		startCancel()
		cancel()
		socks.Close()
		return nil, err
	}
	s.ctx = ctx
	if err := s.connectFirstContext(startCtx); err != nil {
		startCancel()
		cancel()
		socks.Close()
		return nil, err
	}
	startCancel()
	proxy := &Server{listener: socks, session: s, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(proxy.done)
		if err := serveSocks(socks, s); err != nil && ctx.Err() == nil {
			log.Printf("SSH SOCKS listener stopped: %v", err)
		}
	}()
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
