package xray

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestScannerProbePingAndCleanup(t *testing.T) {
	probe, err := newScannerProbe()
	if err != nil {
		t.Fatal(err)
	}

	defer probe.close()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	probe.template = json.RawMessage(`{"protocol":"freedom","settings":{}}`)
	probe.url = server.URL
	const workers = 4
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			delay, err := testScannerPing(probe, context.Background(), "127.0.0.1")
			if err != nil || delay < 0 {
				t.Errorf("ping returned %v, %v", delay, err)
			}
		}()
	}
	wg.Wait()
	if got := requests.Load(); got != workers*2 {
		t.Fatalf("got %d requests, want %d", got, workers*2)
	}
	if got := len(probe.manager.ListHandlers(context.Background())); got != 0 {
		t.Fatalf("got %d remaining outbound handlers", got)
	}
}

func TestScannerProbeRejectsUnhealthyHTTPStatus(t *testing.T) {
	probe, err := newScannerProbe()
	if err != nil {
		t.Fatal(err)
	}
	defer probe.close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	probe.template = json.RawMessage(`{"protocol":"freedom","settings":{}}`)
	probe.url = server.URL
	if _, err := testScannerPing(probe, context.Background(), "127.0.0.1"); err == nil {
		t.Fatal("unhealthy HTTP status was accepted")
	}
}

func TestScannerProbeExcludesVPN(t *testing.T) {
	probe, err := newScannerProbe()
	if err != nil {
		t.Fatal(err)
	}
	controller := NewController()
	if err := controller.Start(scannerBaseConfig, 0); err == nil {
		probe.close()
		t.Fatal("VPN started while the scanner was running")
	}
	if err := probe.close(); err != nil {
		t.Fatal(err)
	}
	if err := controller.Start(scannerBaseConfig, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := newScannerProbe(); err == nil {
		controller.Stop()
		t.Fatal("scanner started while the VPN was running")
	}
	if err := controller.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestScannerBridgeRunsTunneledPing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	host, portText, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	options, err := json.Marshal(map[string]any{
		"protocol": "vless", "sni": host, "port": port, "cidrs": []string{fmt.Sprintf("%s/32", host)},
		"probeCount": 1, "outputCount": 1, "attemptCount": 1, "attemptInterval": 10, "probeInterval": 100,
		"probeUrl": server.URL, "outbound": map[string]any{"protocol": "freedom", "settings": map[string]any{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	bridge := NewScannerBridge()
	if err := bridge.StartScan(string(options)); err != "" {
		t.Fatal(err)
	}
	defer bridge.CloseScan()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		payload := bridge.PollEvent(100)
		if payload == "" {
			continue
		}
		var event struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			t.Fatal(err)
		}
		switch event.Type {
		case "error":
			t.Fatalf("scan failed: %s", event.Message)
		case "done":
			return
		}
	}
	t.Fatal("scanner did not finish")
}

func TestScannerProbeRejectsInvalidTemplate(t *testing.T) {
	probe, err := newScannerProbe()
	if err != nil {
		t.Fatal(err)
	}

	defer probe.close()
	for _, template := range []string{"null", "{}", `{"settings":null}`} {
		probe.template = json.RawMessage(template)
		if _, err := probe.Open(context.Background(), "127.0.0.1"); err == nil {
			t.Errorf("accepted %s", template)
		}
	}
}

func TestScannerProbeCancellationCleansUpHandler(t *testing.T) {
	probe, err := newScannerProbe()
	if err != nil {
		t.Fatal(err)
	}
	defer probe.close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	probe.template = json.RawMessage(`{"protocol":"freedom","settings":{}}`)
	if _, err := probe.Open(ctx, "127.0.0.1"); err == nil {
		t.Fatal("canceled ping succeeded")
	}
	if got := len(probe.manager.ListHandlers(context.Background())); got != 0 {
		t.Fatalf("got %d remaining outbound handlers", got)
	}
}

func TestScannerProbeBuildsVLESSAndMASQUEOutbounds(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := newScannerProbe()
	if err != nil {
		t.Fatal(err)
	}
	defer probe.close()
	for _, protocol := range []string{"vless", "h2", "h3"} {
		t.Run(protocol, func(t *testing.T) {
			config := map[string]any{
				"protocol":       "vless",
				"settings":       map[string]any{"address": "example.com", "port": 443, "id": "6f0c3d7e-1b2a-4c5d-8e9f-0a1b2c3d4e5f", "encryption": "none"},
				"streamSettings": map[string]any{"network": "ws", "wsSettings": map[string]any{"host": "example.com", "path": "/test?ed=2560"}, "security": "tls", "tlsSettings": map[string]any{"serverName": "example.com", "alpn": []string{"http/1.1"}, "fingerprint": "chrome"}},
			}
			if protocol != "vless" {
				config = map[string]any{
					"protocol": "masque",
					"settings": map[string]any{"address": "example.com", "port": 443},
					"streamSettings": map[string]any{
						"network": "masque", "security": "tls",
						"tlsSettings": map[string]any{"serverName": "example.com", "alpn": []string{protocol}},
						"masqueSettings": map[string]any{"warp": map[string]any{
							"privateKey": base64.StdEncoding.EncodeToString(private),
							"publicKey":  base64.StdEncoding.EncodeToString(public),
							"address":    []string{"172.16.0.2", "2606:4700:110:1::2"},
						}},
					},
				}
			}
			raw, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			probe.template = raw
			probe.url = "http://127.0.0.1:1"
			session, err := probe.Open(context.Background(), "127.0.0.1")
			if err != nil {
				t.Fatalf("outbound creation failed: %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err = session.Ping(ctx)
			if closeErr := session.Close(); closeErr != nil {
				t.Fatal(closeErr)
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("outbound failed before the canceled HTTP request: %v", err)
			}
		})
	}
}
