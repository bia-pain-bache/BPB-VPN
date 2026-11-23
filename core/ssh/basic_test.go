package sshproxy

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// testServer runs an SSH server on localhost with password auth and direct-tcpip forwarding.
func testServer(t *testing.T, password string) (addr string, key ssh.PublicKey) {
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
			go serveConn(conn, cfg)
		}
	}()
	return ln.Addr().String(), signer.PublicKey()
}

func serveConn(conn net.Conn, cfg *ssh.ServerConfig) {
	_, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "direct-tcpip" {
			nc.Reject(ssh.UnknownChannelType, "")
			continue
		}
		var msg struct {
			Host     string
			Port     uint32
			OrigHost string
			OrigPort uint32
		}
		ssh.Unmarshal(nc.ExtraData(), &msg)
		target, err := net.Dial("tcp", net.JoinHostPort(msg.Host, strconv.Itoa(int(msg.Port))))
		if err != nil {
			nc.Reject(ssh.ConnectionFailed, err.Error())
			continue
		}
		ch, creqs, _ := nc.Accept()
		go ssh.DiscardRequests(creqs)
		go func() { io.Copy(ch, target); ch.Close() }()
		go func() { io.Copy(target, ch); target.Close() }()
	}
}

func echoServer(t *testing.T) string {
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
			go func() { io.Copy(c, c); c.Close() }()
		}
	}()
	return ln.Addr().String()
}

// socksConnect does a SOCKS5 CONNECT to target through the proxy at proxyAddr.
func socksConnect(t *testing.T, proxyAddr, target string) net.Conn {
	c, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatal(err)
	}
	c.SetDeadline(time.Now().Add(10 * time.Second))
	c.Write([]byte{5, 1, 0})
	io.ReadFull(c, make([]byte, 2))
	host, portStr, _ := net.SplitHostPort(target)
	port, _ := strconv.Atoi(portStr)
	req := []byte{5, 1, 0, 3, byte(len(host))}
	req = append(req, host...)
	req = binary.BigEndian.AppendUint16(req, uint16(port))
	c.Write(req)
	rep := make([]byte, 10)
	if _, err := io.ReadFull(c, rep); err != nil || rep[1] != repSucceeded {
		t.Fatalf("CONNECT failed: %v %v", err, rep)
	}
	return c
}

func TestSocksOverSSH(t *testing.T) {
	server, key := testServer(t, "secret")
	echo := echoServer(t)
	s, err := newSession(config{Server: server, User: "u", Password: "secret", HostKey: ssh.FingerprintSHA256(key)})
	if err != nil {
		t.Fatal(err)
	}
	s.connectFirst()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go serveSocks(ln, s)

	c := socksConnect(t, ln.Addr().String(), echo)
	defer c.Close()
	c.Write([]byte("hello"))
	got := make([]byte, 5)
	if _, err := io.ReadFull(c, got); err != nil || string(got) != "hello" {
		t.Fatalf("echo through SSH: %q %v", got, err)
	}
}

func TestHostKeyPinning(t *testing.T) {
	server, key := testServer(t, "secret")

	// Unpinned: accepted, and reported.
	s, _ := newSession(config{Server: server, User: "u", Password: "secret"})
	c, err := s.connect()
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if s.cfg.HostKey != ssh.FingerprintSHA256(key) {
		t.Fatalf("pinned %q, want the server's key", s.cfg.HostKey)
	}

	// Pinned to another key: refused, and not retried.
	s, _ = newSession(config{Server: server, User: "u", Password: "secret", HostKey: "SHA256:something-else"})
	_, err = s.connect()
	var auth errAuth
	if !errors.As(err, &auth) {
		t.Fatalf("got %v, want a host key error", err)
	}
}

func TestWrongPassword(t *testing.T) {
	server, key := testServer(t, "secret")
	s, _ := newSession(config{Server: server, User: "u", Password: "nope", HostKey: ssh.FingerprintSHA256(key)})
	_, err := s.connect()
	var auth errAuth
	if !errors.As(err, &auth) {
		t.Fatalf("got %v, want an auth error (not retried)", err)
	}
}

func TestEncryptedPrivateKey(t *testing.T) {
	_, clientKey, _ := ed25519.GenerateKey(rand.Reader)
	block, err := ssh.MarshalPrivateKeyWithPassphrase(clientKey, "", []byte("open sesame"))
	if err != nil {
		t.Fatal(err)
	}
	pemKey := string(pem.EncodeToMemory(block))

	if _, err := parseKey(pemKey, ""); err == nil || !strings.Contains(err.Error(), "set its passphrase") {
		t.Fatalf("no passphrase: got %v", err)
	}
	if _, err := parseKey(pemKey, "wrong"); err == nil || !strings.Contains(err.Error(), "wrong passphrase") {
		t.Fatalf("wrong passphrase: got %v", err)
	}

	// The right passphrase logs in with the key.
	signer, _ := ssh.NewSignerFromKey(clientKey)
	server, hostKey := testServerWithKey(t, signer.PublicKey())
	s, err := newSession(config{
		Server: server, User: "u", PrivateKey: pemKey, Passphrase: "open sesame", HostKey: ssh.FingerprintSHA256(hostKey),
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.connect()
	if err != nil {
		t.Fatalf("key login: %v", err)
	}
	c.Close()
}

// testServerWithKey is testServer accepting only the client key [allowed].
func testServerWithKey(t *testing.T, allowed ssh.PublicKey) (addr string, key ssh.PublicKey) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
			if string(k.Marshal()) == string(allowed.Marshal()) {
				return nil, nil
			}
			return nil, errors.New("unknown key")
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
			go serveConn(conn, cfg)
		}
	}()
	return ln.Addr().String(), signer.PublicKey()
}
