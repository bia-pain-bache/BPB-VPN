package xray

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"
)

func TestScannerWireGuardDialsCandidateEndpoints(t *testing.T) {
	first, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	port := first.LocalAddr().(*net.UDPAddr).Port
	second, err := net.ListenPacket("udp4", fmt.Sprintf("127.0.0.2:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{
		"protocol": "wireguard",
		"settings": map[string]any{
			"secretKey": base64.StdEncoding.EncodeToString(private.Bytes()),
			"address":   []string{"172.16.0.2/32"},
			"peers": []map[string]any{{
				"publicKey": base64.StdEncoding.EncodeToString(peer.PublicKey().Bytes()),
				"endpoint":  fmt.Sprintf("127.0.0.1:%d", port),
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	probe, err := newScannerProbe()
	if err != nil {
		t.Fatal(err)
	}
	defer probe.close()
	probe.template = raw
	probe.url = "http://example.com/"
	firstSession, err := probe.Open(context.Background(), "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	defer firstSession.Close()
	secondSession, err := probe.Open(context.Background(), "127.0.0.2")
	if err != nil {
		t.Fatal(err)
	}
	defer secondSession.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var pending sync.WaitGroup
	for _, s := range []interface {
		Ping(context.Context) (time.Duration, error)
	}{firstSession, secondSession} {
		pending.Add(1)
		go func() { defer pending.Done(); s.Ping(ctx) }()
	}
	received := make(chan error, 2)
	for _, listener := range []net.PacketConn{first, second} {
		go func(listener net.PacketConn) {
			listener.SetReadDeadline(time.Now().Add(5 * time.Second))
			buffer := make([]byte, 2048)
			_, _, err := listener.ReadFrom(buffer)
			received <- err
		}(listener)
	}
	for range 2 {
		if err := <-received; err != nil {
			cancel()
			pending.Wait()
			t.Fatalf("WireGuard candidate received no packets: %v", err)
		}
	}
	cancel()
	pending.Wait()
}

func TestScannerVLESSOutboundsDialTheirOwnCandidates(t *testing.T) {
	first, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	port := first.Addr().(*net.TCPAddr).Port
	second, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.2:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	probe, err := newScannerProbe()
	if err != nil {
		t.Fatal(err)
	}
	defer probe.close()
	probe.template = json.RawMessage(fmt.Sprintf(`{"protocol":"vless","settings":{"address":"example.com","port":%d,"id":"6f0c3d7e-1b2a-4c5d-8e9f-0a1b2c3d4e5f","encryption":"none"}}`, port))
	probe.url = "http://example.com/"
	firstSession, err := probe.Open(context.Background(), "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	defer firstSession.Close()
	secondSession, err := probe.Open(context.Background(), "127.0.0.2")
	if err != nil {
		t.Fatal(err)
	}
	defer secondSession.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	var pending sync.WaitGroup
	for _, s := range []*scannerSession{firstSession.(*scannerSession), secondSession.(*scannerSession)} {
		pending.Add(1)
		go func(s *scannerSession) {
			defer pending.Done()
			s.Ping(ctx)
		}(s)
	}
	accepted := make(chan string, 8)
	for _, listener := range []net.Listener{first, second} {
		go func(listener net.Listener) {
			for {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				accepted <- listener.Addr().String()
				conn.Close()
			}
		}(listener)
	}
	counts := make(map[string]int)
	for range 4 {
		select {
		case address := <-accepted:
			counts[address]++
		case <-ctx.Done():
			t.Fatalf("candidate requests did not complete: %v", counts)
		}
	}
	cancel()
	pending.Wait()
	if counts[first.Addr().String()] != 2 || counts[second.Addr().String()] != 2 {
		t.Fatalf("both requests must use their own candidate: %v", counts)
	}
}
