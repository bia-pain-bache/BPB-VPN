// Package networking is a local gateway that forwards HTTP requests to a few Cloudflare API
// hosts over TLS with Encrypted Client Hello, for when the direct route is blocked. The app
// sends plain HTTP to the gateway with the real host in X-SNI (see lib/services/ech_client.dart).
package networking

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	// DefaultAddress is where the app's EchClient looks for the gateway.
	DefaultAddress = "127.0.0.1:10810"

	// ErrorHeader marks a response the gateway made itself (the upstream couldn't be
	// reached), as opposed to one the upstream sent.
	ErrorHeader = "X-ECH-Error"

	ttl   = time.Hour // local lifetime of learned ECH configs
	outer = "cloudflare-ech.com"
)

// allowedHosts keeps the gateway from being an open proxy for other apps on the phone.
var allowedHosts = map[string]bool{
	"api.cloudflare.com":       true,
	"api.cloudflareclient.com": true,
}

type dialKey struct{}

func u16(n int) []byte { return []byte{byte(n >> 8), byte(n)} }

func fakeECH(name string) ([]byte, error) {
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	id := make([]byte, 1)
	rand.Read(id)
	c := append([]byte{}, id...)
	c = append(c, 0x00, 0x20)
	c = append(c, u16(32)...)
	c = append(c, k.PublicKey().Bytes()...)
	c = append(c, u16(4)...)
	c = append(c, 0, 1, 0, 1)
	c = append(c, 64, byte(len(name)))
	c = append(c, name...)
	c = append(c, 0, 0)
	cfg := append([]byte{0xfe, 0x0d}, u16(len(c))...)
	cfg = append(cfg, c...)
	return append(u16(len(cfg)), cfg...), nil
}

type entry struct {
	list []byte
	exp  time.Time
}

type echDialer struct {
	mu    sync.Mutex
	cache map[string]entry
}

func (d *echDialer) handshake(ctx context.Context, addr, sni string, list []byte) (net.Conn, error) {
	raw, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	c := tls.Client(raw, &tls.Config{
		ServerName:                     sni,
		MinVersion:                     tls.VersionTLS13,
		NextProtos:                     []string{"http/1.1"},
		EncryptedClientHelloConfigList: list,
	})
	if err := c.HandshakeContext(ctx); err != nil {
		raw.Close()
		return nil, err
	}
	return c, nil
}

func (d *echDialer) dialTLS(ctx context.Context, _, hostport string) (net.Conn, error) {
	sni, _, err := net.SplitHostPort(hostport)
	if err != nil {
		return nil, err
	}
	addr := hostport
	if v, ok := ctx.Value(dialKey{}).(string); ok && v != "" {
		addr = v
	}

	d.mu.Lock()
	e, ok := d.cache[sni]
	d.mu.Unlock()
	list := e.list
	if !ok || time.Now().After(e.exp) {
		if list, err = fakeECH(outer); err != nil {
			return nil, err
		}
	}

	conn, err := d.handshake(ctx, addr, sni, list)
	var rej *tls.ECHRejectionError
	if errors.As(err, &rej) {
		if len(rej.RetryConfigList) == 0 {
			return nil, errors.New(sni + ": ECH rejected, no retry_configs")
		}
		d.mu.Lock()
		d.cache[sni] = entry{rej.RetryConfigList, time.Now().Add(ttl)}
		d.mu.Unlock()
		return d.handshake(ctx, addr, sni, rej.RetryConfigList)
	}
	return conn, err
}

// Gateway is the running local server.
type Gateway struct {
	server *http.Server
}

var (
	runMu   sync.Mutex
	running *Gateway
)

// Start runs the gateway on address (DefaultAddress when empty). It's a no-op when already
// running.
func Start(address string) error {
	runMu.Lock()
	defer runMu.Unlock()
	if running != nil {
		return nil
	}
	if address == "" {
		address = DefaultAddress
	}
	ln, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	g := &Gateway{server: &http.Server{Handler: newHandler(), ReadHeaderTimeout: 15 * time.Second}}
	running = g
	go func() {
		g.server.Serve(ln)
		runMu.Lock()
		if running == g {
			running = nil
		}
		runMu.Unlock()
	}()
	return nil
}

// Stop shuts the gateway down.
func Stop() {
	runMu.Lock()
	g := running
	running = nil
	runMu.Unlock()
	if g != nil {
		g.server.Close()
	}
}

func gatewayError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set(ErrorHeader, "1")
	http.Error(w, msg, code)
}

func newHandler() http.Handler {
	d := &echDialer{cache: map[string]entry{}}
	rt := &http.Transport{
		DialTLSContext:     d.dialTLS,
		DisableKeepAlives:  true,
		DisableCompression: true,
		TLSNextProto:       map[string]func(string, *tls.Conn) http.RoundTripper{},
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sni := r.Header.Get("X-SNI")
		if sni == "" {
			gatewayError(w, "missing X-SNI", http.StatusBadRequest)
			return
		}
		if !allowedHosts[sni] {
			gatewayError(w, sni+": host not allowed", http.StatusForbidden)
			return
		}
		dial := r.Header.Get("X-Dial")
		host := r.Header.Get("X-Host")
		if host == "" {
			host = sni
		}
		r.Header.Del("X-SNI")
		r.Header.Del("X-Dial")
		r.Header.Del("X-Host")

		r.URL.Scheme, r.URL.Host, r.Host, r.RequestURI = "https", sni, host, ""
		r = r.WithContext(context.WithValue(r.Context(), dialKey{}, dial))

		resp, err := rt.RoundTrip(r)
		if err != nil {
			gatewayError(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	})
}
