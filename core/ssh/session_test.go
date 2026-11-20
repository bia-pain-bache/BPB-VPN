package sshproxy

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// fast shortens one session's timeouts.
func fast(s *session, dial, keepalive, handshake time.Duration) {
	s.dialTimeout, s.keepaliveTimeout, s.handshakeTimeout = dial, keepalive, handshake
}

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuf) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

func captureLog(t *testing.T) *lockedBuf {
	buf := &lockedBuf{}
	log.SetOutput(buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return buf
}

// rbServer is an SSH server on localhost that accepts one password. It forwards nothing, which
// these tests don't need; it keeps this file independent of the helpers in session_test.go.
func rbServer(t *testing.T, password string) (addr string, key ssh.PublicKey) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(_ ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
			if string(p) == password {
				return nil, nil
			}
			return nil, errors.New("wrong password")
		},
	}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, chans, reqs, err := ssh.NewServerConn(conn, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for nc := range chans {
					nc.Reject(ssh.Prohibited, "")
				}
			}()
		}
	}()
	return ln.Addr().String(), signer.PublicKey()
}

// silentListener accepts TCP connections and never says anything.
func silentListener(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			if _, err := ln.Accept(); err != nil {
				return
			}
		}
	}()
	return ln.Addr().String()
}

// hungServer completes SSH handshakes but never answers global requests (keepalives): what a
// path that silently stopped forwarding looks like.
func hungServer(t *testing.T) string {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	cfg := &ssh.ServerConfig{PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) { return nil, nil }}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, chans, _, err := ssh.NewServerConn(c, cfg) // requests never read or answered
				if err != nil {
					return
				}
				for range chans {
				}
			}()
		}
	}()
	return ln.Addr().String()
}

func TestConnectTimesOutOnSilentPeer(t *testing.T) {
	s, _ := newSession(config{Server: silentListener(t), User: "u", Password: "p"})
	fast(s, 300*time.Millisecond, time.Second, time.Second)
	start := time.Now()
	_, err := s.connect()
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("connect: %v after %s, want an error within the dial timeout", err, time.Since(start))
	}
}

func TestConnectAbortedByContext(t *testing.T) {
	s, _ := newSession(config{Server: silentListener(t), User: "u", Password: "p"}) // 20s dial timeout
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(150*time.Millisecond, cancel)
	start := time.Now()
	_, err := s.connectContext(ctx)
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("connect: %v after %s, want it to stop when ctx ends", err, time.Since(start))
	}
}

func TestStartTimeoutReportsTarget(t *testing.T) {
	s, _ := newSession(config{Server: silentListener(t), User: "u", Password: "p"})
	fast(s, 300*time.Millisecond, time.Second, time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	err := s.connectFirstContext(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), s.cfg.Server) {
		t.Fatalf("got %v, want a deadline error naming the server", err)
	}
}

func TestKeepaliveTimeoutDropsDeadPath(t *testing.T) {
	buf := captureLog(t)
	s, _ := newSession(config{Server: hungServer(t), User: "u", Password: "p", KeepAliveInterval: 1})
	fast(s, 5*time.Second, 300*time.Millisecond, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s.ctx = ctx
	if err := s.connectFirst(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(buf.String(), "SSH session lost") {
		if time.Now().After(deadline) {
			t.Fatalf("unanswered keepalives never dropped the session. log:\n%s", buf.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// slowDialer ignores ctx: a dial that completes after Stop.
type slowDialer struct{ delay time.Duration }

func (d slowDialer) Dial(network, addr string) (net.Conn, error) {
	time.Sleep(d.delay)
	return net.Dial(network, addr)
}

func TestStopDuringReconnectLeavesNoClient(t *testing.T) {
	server, key := rbServer(t, "secret")
	s, _ := newSession(config{Server: server, User: "u", Password: "secret", HostKey: ssh.FingerprintSHA256(key)})
	ctx, cancel := context.WithCancel(context.Background())
	s.ctx = ctx
	if err := s.connectFirst(); err != nil {
		t.Fatal(err)
	}
	s.dialer = slowDialer{400 * time.Millisecond}

	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	c.Close() // the session drops; drop starts a (slow) reconnect
	time.Sleep(100 * time.Millisecond)
	cancel() // Stop, while it is connecting
	s.close()
	time.Sleep(900 * time.Millisecond)

	s.mu.Lock()
	leaked := s.client != nil
	s.mu.Unlock()
	if leaked {
		t.Fatal("a client was connected after the session was stopped")
	}
}

func TestChangedHostKeyOnReconnectEndsSession(t *testing.T) {
	server, key := rbServer(t, "secret")
	captureLog(t)
	s, _ := newSession(config{Server: server, User: "u", Password: "secret", HostKey: ssh.FingerprintSHA256(key)})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s.ctx = ctx
	if err := s.connectFirst(); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.cfg.HostKey = "SHA256:not-the-servers" // as if the server's key changed
	c := s.client
	s.mu.Unlock()
	c.Close()

	select {
	case <-s.dead:
	case <-time.After(3 * time.Second):
		t.Fatal("the session did not end after a changed host key")
	}
	start := time.Now()
	_, err := s.dial("127.0.0.1:1")
	if err == nil || !strings.Contains(err.Error(), "host key changed") || time.Since(start) > time.Second {
		t.Fatalf("dial: %v after %s, want an immediate host key error", err, time.Since(start))
	}
}

func TestSocksHandshakeTimeout(t *testing.T) {
	s, _ := newSession(config{Server: "127.0.0.1:1", User: "u", Password: "p"})
	fast(s, time.Second, time.Second, 200*time.Millisecond)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go serveSocks(ln, s)

	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.Read(make([]byte, 1)); err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("read: %v, want the server to close the stalled connection", err)
	}
}