package sshproxy

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

// SOCKS5 (RFC 1928), no authentication, CONNECT only: SSH carries TCP.
const (
	socksVersion   = 0x05
	cmdConnect     = 0x01
	atypIPv4       = 0x01
	atypDomain     = 0x03
	atypIPv6       = 0x04
	repSucceeded   = 0x00
	repFailure     = 0x01
	repHostUnreach = 0x04
	repCmdNotSupp  = 0x07
)

// handshakeTimeout bounds a client's greeting and request; a client that connects and then
// stalls would otherwise hold a goroutine and a socket forever.
const handshakeTimeout = 10 * time.Second

// serveSocks serves until the listener fails.
func serveSocks(ln net.Listener, s *session) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go func() {
			defer conn.Close()
			handleSocks(conn, s)
		}()
	}
}

func handleSocks(conn net.Conn, s *session) {
	conn.SetDeadline(time.Now().Add(s.handshakeTimeout))

	// Greeting: version, methods; "no authentication" only.
	head := make([]byte, 2)
	if _, err := io.ReadFull(conn, head); err != nil || head[0] != socksVersion {
		return
	}
	if _, err := io.ReadFull(conn, make([]byte, head[1])); err != nil {
		return
	}
	if _, err := conn.Write([]byte{socksVersion, 0x00}); err != nil {
		return
	}

	// Request: version, command, reserved, address.
	req := make([]byte, 3)
	if _, err := io.ReadFull(conn, req); err != nil || req[0] != socksVersion {
		return
	}
	host, port, err := readAddr(conn)
	if err != nil {
		return
	}
	if req[1] != cmdConnect {
		reply(conn, repCmdNotSupp)
		return
	}
	// Dialing may take a while (a reconnect); the handshake deadline no longer applies.
	conn.SetDeadline(time.Time{})
	target, err := s.dial(net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		reply(conn, repHostUnreach)
		return
	}
	defer target.Close()
	if reply(conn, repSucceeded) != nil {
		return
	}
	relay(conn, target)
}

func readAddr(r io.Reader) (string, int, error) {
	atyp := make([]byte, 1)
	if _, err := io.ReadFull(r, atyp); err != nil {
		return "", 0, err
	}
	var host string
	switch atyp[0] {
	case atypIPv4, atypIPv6:
		ip := make([]byte, map[byte]int{atypIPv4: 4, atypIPv6: 16}[atyp[0]])
		if _, err := io.ReadFull(r, ip); err != nil {
			return "", 0, err
		}
		host = net.IP(ip).String()
	case atypDomain:
		n := make([]byte, 1)
		if _, err := io.ReadFull(r, n); err != nil {
			return "", 0, err
		}
		name := make([]byte, n[0])
		if _, err := io.ReadFull(r, name); err != nil {
			return "", 0, err
		}
		host = string(name)
	default:
		return "", 0, errors.New("bad address type")
	}
	p := make([]byte, 2)
	if _, err := io.ReadFull(r, p); err != nil {
		return "", 0, err
	}
	return host, int(binary.BigEndian.Uint16(p)), nil
}

// reply sends a reply with an unspecified bound address (0.0.0.0:0).
func reply(conn net.Conn, rep byte) error {
	_, err := conn.Write([]byte{socksVersion, rep, 0x00, atypIPv4, 0, 0, 0, 0, 0, 0})
	return err
}

// relay copies both ways until either side closes.
func relay(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	cp := func(dst, src net.Conn) {
		defer wg.Done()
		io.Copy(dst, src)
		if c, ok := dst.(interface{ CloseWrite() error }); ok {
			c.CloseWrite()
		} else {
			dst.Close()
		}
	}
	go cp(a, b)
	go cp(b, a)
	wg.Wait()
}