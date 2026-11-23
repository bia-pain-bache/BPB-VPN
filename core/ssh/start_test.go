package sshproxy

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// stServer is an SSH server on addr that accepts one password and forwards nothing.
func stServer(t *testing.T, addr, password string) (string, ssh.PublicKey) {
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
	ln, err := net.Listen("tcp", addr)
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

// stStart points Start at a free SOCKS port and sets how long it waits for the first connection.
func stStart(t *testing.T, wait time.Duration) {
	oldAddr, oldWait := socksListenAddress, firstConnectWait
	socksListenAddress, firstConnectWait = "127.0.0.1:0", wait
	t.Cleanup(func() { socksListenAddress, firstConnectWait = oldAddr, oldWait })
}

func stConfig(addr, password string) Config {
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	return Config{Host: host, Port: port, User: "u", Password: password}
}

func TestStartReturnsAnAuthenticationFailure(t *testing.T) {
	stStart(t, 10*time.Second)
	server, _ := stServer(t, "127.0.0.1:0", "secret")
	start := time.Now()
	srv, err := Start(stConfig(server, "wrong"))
	if err == nil || !strings.Contains(err.Error(), "wrong username, password or key") || time.Since(start) > 3*time.Second {
		t.Fatalf("Start: %v after %s, want a fast, plain authentication error", err, time.Since(start))
	}
	if strings.Contains(err.Error(), "ssh:") || len(err.Error()) > 60 {
		t.Fatalf("the error is not short and plain: %q", err)
	}
	if srv != nil {
		t.Fatal("Start returned a server together with the error")
	}
}

func TestStartReturnsOnceConnected(t *testing.T) {
	stStart(t, 10*time.Second)
	server, _ := stServer(t, "127.0.0.1:0", "secret")
	start := time.Now()
	srv, err := Start(stConfig(server, "secret"))
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	srv.session.mu.Lock()
	connected := srv.session.client != nil
	srv.session.mu.Unlock()
	if !connected || time.Since(start) > 3*time.Second {
		t.Fatalf("connected=%v after %s, want a connected session right away", connected, time.Since(start))
	}
}

func TestStartKeepsRetryingAnUnreachableServer(t *testing.T) {
	stStart(t, 500*time.Millisecond)
	// A port with nothing listening.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()

	start := time.Now()
	srv, err := Start(stConfig(addr, "secret"))
	if err != nil || time.Since(start) > 3*time.Second {
		t.Fatalf("Start: %v after %s, want no error after the short wait", err, time.Since(start))
	}
	defer srv.Stop()

	// The server comes up later; the background retry must find it.
	stServer(t, addr, "secret")
	srv.session.mu.Lock()
	ready := srv.session.ready
	srv.session.mu.Unlock()
	select {
	case <-ready:
	case <-time.After(8 * time.Second):
		t.Fatal("the session never connected after the server came up")
	}
}

func TestStartLearnsAndPinsTheHostKey(t *testing.T) {
	stStart(t, 10*time.Second)
	server, key := stServer(t, "127.0.0.1:0", "secret")
	fp := ssh.FingerprintSHA256(key)

	// Unpinned: starts, and reports the key it learned.
	srv, err := Start(stConfig(server, "secret"))
	if err != nil {
		t.Fatal(err)
	}
	if srv.HostKey() != fp {
		t.Fatalf("HostKey %q, want %q", srv.HostKey(), fp)
	}
	srv.Stop()

	// Pinned to the right key: starts.
	pinned := stConfig(server, "secret")
	pinned.HostKey = fp
	srv, err = Start(pinned)
	if err != nil {
		t.Fatal(err)
	}
	srv.Stop()

	// Pinned to another key: refused at once, as a host key error.
	wrong := stConfig(server, "secret")
	wrong.HostKey = "SHA256:something-else"
	start := time.Now()
	if _, err := Start(wrong); err == nil || !strings.Contains(err.Error(), "host key changed") || time.Since(start) > 3*time.Second {
		t.Fatalf("Start with a wrong pin: %v after %s", err, time.Since(start))
	}
}

func TestHostKeyOfNoServerIsEmpty(t *testing.T) {
	var s *Server
	if s.HostKey() != "" {
		t.Fatal("a nil Server has no host key")
	}
}

func TestPrivateKeyErrorsAreShortAndPlain(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	public := string(ssh.MarshalAuthorizedKey(signer.PublicKey()))
	cases := []struct{ name, key, want string }{
		{"not a key", "hello", "isn't valid"},
		{"a truncated key", "-----BEGIN OPENSSH PRIVATE KEY-----\nAAAA\n", "isn't valid"},
		{"junk inside the BEGIN and END lines", "-----BEGIN OPENSSH PRIVATE KEY-----\nAAAAAAAA\n-----END OPENSSH PRIVATE KEY-----\n", "can't be read"},
		{"a public key", public, "public key"},
		{"a PuTTY key", "PuTTY-User-Key-File-3: ssh-ed25519\nEncryption: none\n", "PuTTY"},
	}
	for _, c := range cases {
		_, err := parseKey(c.key, "")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want it to say %q", c.name, err, c.want)
			continue
		}
		if strings.Contains(err.Error(), "ssh:") || len(err.Error()) > 100 {
			t.Errorf("%s: not short and plain: %q", c.name, err)
		}
	}
}
