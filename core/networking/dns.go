package networking

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
)

type DNSRequest struct {
	Host      string   `json:"host"`       // server hostname; TLS name for doh/doq
	IPs       []string `json:"ips"`        // raced; optional for tcp/doh/doq (host is then resolved locally), mandatory for udp
	Type      string   `json:"type"`       // tcp, udp, doh, doq
	Path      string   `json:"path"`       // doh/doq; default /dns-query
	Endpoint  string   `json:"endpoint"`   // HEAD target; default http://cp.cloudflare.com/generate_204
	TimeoutMs int      `json:"timeout_ms"` // default 5000
}

type DNSResponse struct {
	OK      bool     `json:"ok"`
	Latency *float64 `json:"latency,omitempty"` // ms spent in the DNS lookup; set only when ok
}

var dnsPorts = map[string]string{"udp": "53", "tcp": "53", "doh": "443", "doq": "853"}

// CheckDNS races all IPs of the server. Each racer resolves the endpoint host through
// that IP, then sends a HEAD request that must return 204. The first racer to succeed wins.
func CheckDNS(ctx context.Context, r DNSRequest) DNSResponse {
	typ := strings.ToLower(r.Type)
	if _, ok := dnsPorts[typ]; !ok || (r.Host == "" && (typ == "doh" || typ == "doq")) {
		return DNSResponse{}
	}
	var targets []string // IPs to race; the host itself (resolved locally) when there are none
	for _, s := range r.IPs {
		if ip, err := netip.ParseAddr(s); err == nil {
			targets = append(targets, ip.String())
		}
	}
	if len(targets) == 0 {
		if typ == "udp" || r.Host == "" {
			return DNSResponse{}
		}
		targets = []string{r.Host}
	}
	if r.Endpoint == "" {
		r.Endpoint = "http://cp.cloudflare.com/generate_204"
	}
	if r.Path == "" {
		r.Path = "/dns-query"
	} else if r.Path[0] != '/' {
		r.Path = "/" + r.Path
	}
	if r.TimeoutMs <= 0 {
		r.TimeoutMs = 5000
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(r.TimeoutMs)*time.Millisecond)
	defer cancel() // also aborts the losers

	type result struct {
		d  time.Duration
		ok bool
	}
	ch := make(chan result, len(targets))
	for _, t := range targets {
		go func() {
			d, ok := checkDNSOne(ctx, newDNSLookup(typ, r.Host, r.Path, t), r.Endpoint)
			ch <- result{d, ok}
		}()
	}
	for range targets {
		if res := <-ch; res.ok {
			ms := float64(res.d.Microseconds()) / 1000
			return DNSResponse{OK: true, Latency: &ms}
		}
	}
	return DNSResponse{}
}

// checkDNSOne returns the lookup time when the HEAD request (after redirects)
// returns 200 OK or 204 No Content.
func checkDNSOne(ctx context.Context, lookup dnsLookup, endpoint string) (time.Duration, bool) {
	var dnsNs atomic.Int64
	client := &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: true,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				h, p, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				if _, err := netip.ParseAddr(h); err != nil {
					t := time.Now()
					ips, err := lookup(ctx, h)
					dnsNs.Store(int64(time.Since(t)))
					if err != nil {
						return nil, err
					}
					h = ips[0].String()
				}
				var d net.Dialer
				return d.DialContext(ctx, network, net.JoinHostPort(h, p))
			},
		},
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) > 3 {
				return errors.New("stopped after 3 redirects")
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, endpoint, nil)
	if err != nil {
		return 0, false
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, false
	}
	resp.Body.Close()
	d := time.Duration(dnsNs.Load())
	// d == 0 means no lookup ran (endpoint host is an IP literal): nothing was measured
	statusOK := resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent
	return d, statusOK && d > 0
}

// ---- lookups ----

type dnsLookup func(ctx context.Context, host string) ([]netip.Addr, error)

func newDNSLookup(typ, host, path, target string) dnsLookup {
	addr := net.JoinHostPort(target, dnsPorts[typ])
	switch typ {
	case "udp", "tcp":
		r := &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				if typ == "tcp" {
					network = "tcp"
				}
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		}
		return func(ctx context.Context, h string) ([]netip.Addr, error) {
			return r.LookupNetIP(ctx, "ip4", dnsFQDN(h))
		}
	case "doh":
		return dohLookup(host, path, addr)
	default:
		return doqLookup(host, addr)
	}
}

func dohLookup(host, path, addr string) dnsLookup {
	return func(ctx context.Context, h string) ([]netip.Addr, error) {
		q, err := buildDNSQuery(h)
		if err != nil {
			return nil, err
		}
		client := &http.Client{Transport: &http.Transport{
			DisableKeepAlives: true,
			ForceAttemptHTTP2: true,
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		}}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+host+path, bytes.NewReader(q))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/dns-message")
		req.Header.Set("Accept", "application/dns-message")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("doh: status %d", resp.StatusCode)
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 65535))
		if err != nil {
			return nil, err
		}
		return parseDNSA(b)
	}
}

func doqLookup(host, addr string) dnsLookup {
	return func(ctx context.Context, h string) ([]netip.Addr, error) {
		q, err := buildDNSQuery(h)
		if err != nil {
			return nil, err
		}
		conn, err := quic.DialAddr(ctx, addr, &tls.Config{
			ServerName: host,
			NextProtos: []string{"doq"},
		}, nil)
		if err != nil {
			return nil, err
		}
		defer conn.CloseWithError(0, "")

		st, err := conn.OpenStreamSync(ctx)
		if err != nil {
			return nil, err
		}
		if dl, ok := ctx.Deadline(); ok {
			st.SetDeadline(dl)
		}
		if _, err := st.Write(append([]byte{byte(len(q) >> 8), byte(len(q))}, q...)); err != nil {
			return nil, err
		}
		st.Close() // FIN: query sent
		var l [2]byte
		if _, err := io.ReadFull(st, l[:]); err != nil {
			return nil, err
		}
		b := make([]byte, int(l[0])<<8|int(l[1]))
		if _, err := io.ReadFull(st, b); err != nil {
			return nil, err
		}
		return parseDNSA(b)
	}
}

// buildDNSQuery packs an A/IN query with ID 0 (required by RFC 8484 and RFC 9250).
func buildDNSQuery(host string) ([]byte, error) {
	host = strings.TrimSuffix(host, ".")
	if host == "" || len(host) > 253 {
		return nil, fmt.Errorf("invalid host %q", host)
	}
	b := []byte{0, 0, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0} // ID 0, RD, QDCOUNT 1
	for _, l := range strings.Split(host, ".") {
		if l == "" || len(l) > 63 {
			return nil, fmt.Errorf("invalid host %q", host)
		}
		b = append(b, byte(len(l)))
		b = append(b, l...)
	}
	return append(b, 0, 0, 1, 0, 1), nil // root, QTYPE A, QCLASS IN
}

// parseDNSA extracts the A records from a DNS response.
func parseDNSA(b []byte) ([]netip.Addr, error) {
	errBad := errors.New("malformed response")
	if len(b) < 12 {
		return nil, errBad
	}
	if rc := b[3] & 0x0f; rc != 0 {
		return nil, fmt.Errorf("rcode %d", rc)
	}
	qd, an := int(b[4])<<8|int(b[5]), int(b[6])<<8|int(b[7])
	off := 12
	skipName := func() bool { // handles compression pointers
		for off < len(b) {
			n := int(b[off])
			switch {
			case n == 0:
				off++
				return true
			case n&0xc0 == 0xc0:
				off += 2
				return true
			default:
				off += 1 + n
			}
		}
		return false
	}
	for i := 0; i < qd; i++ {
		if !skipName() {
			return nil, errBad
		}
		off += 4 // QTYPE, QCLASS
	}
	var out []netip.Addr
	for i := 0; i < an; i++ {
		if !skipName() || off+10 > len(b) {
			return nil, errBad
		}
		typ, class := int(b[off])<<8|int(b[off+1]), int(b[off+2])<<8|int(b[off+3])
		rdlen := int(b[off+8])<<8 | int(b[off+9])
		off += 10
		if off+rdlen > len(b) {
			return nil, errBad
		}
		if typ == 1 && class == 1 && rdlen == 4 {
			out = append(out, netip.AddrFrom4([4]byte(b[off:off+4])))
		}
		off += rdlen
	}
	if len(out) == 0 {
		return nil, errors.New("no A records")
	}
	return out, nil
}

func dnsFQDN(h string) string { return strings.TrimSuffix(h, ".") + "." }
