package networking

import (
	"context"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestBuildDNSQuery(t *testing.T) {
	query, err := buildDNSQuery("google.com.")
	if err != nil {
		t.Fatal(err)
	}
	if len(query) < 12 {
		t.Fatalf("query is too short: %d", len(query))
	}
	if query[0] != 0 || query[1] != 0 {
		t.Fatalf("query ID = %x, want zero", query[:2])
	}
	if query[2]&1 != 1 || binary.BigEndian.Uint16(query[4:6]) != 1 {
		t.Fatalf("query flags/count are invalid: %x", query[:12])
	}
	if got := string(query[13:]); !strings.Contains(got, "google") {
		t.Fatalf("encoded query does not contain google: %x", query)
	}
}

func TestBuildDNSQueryRejectsInvalidNames(t *testing.T) {
	for _, host := range []string{"", ".", "bad..name", strings.Repeat("a", 64) + ".com"} {
		t.Run(fmt.Sprintf("%q", host), func(t *testing.T) {
			if _, err := buildDNSQuery(host); err == nil {
				t.Fatalf("buildDNSQuery(%q) succeeded", host)
			}
		})
	}
}

func TestParseDNSA(t *testing.T) {
	response := dnsAResponse(netip.MustParseAddr("8.8.8.8"))
	got, err := parseDNSA(response)
	if err != nil {
		t.Fatal(err)
	}
	want := []netip.Addr{netip.MustParseAddr("8.8.8.8")}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("parseDNSA() = %v, want %v", got, want)
	}
}

func TestParseDNSARejectsMalformedAndUnsuccessfulResponses(t *testing.T) {
	for name, response := range map[string][]byte{
		"short":        make([]byte, 11),
		"server error": {0, 0, 0, 3, 0, 0, 0, 0, 0, 0, 0, 0},
		"no answers":   {0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseDNSA(response); err == nil {
				t.Fatalf("parseDNSA(%s) succeeded", name)
			}
		})
	}
}

func TestCheckDNSRejectsInvalidRequests(t *testing.T) {
	for name, request := range map[string]DNSRequest{
		"unknown type":     {Type: "https", Host: "dns.example"},
		"DoH without host": {Type: "doh"},
		"DoQ without host": {Type: "doq"},
		"UDP without IP":   {Type: "udp", Host: "dns.example"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := CheckDNS(context.Background(), request); got.OK || got.Latency != nil {
				t.Fatalf("CheckDNS() = %+v, want failure", got)
			}
		})
	}
}

func TestCheckDNSOneAcceptsOnly200And204(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNoContent, http.StatusNotFound} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var requests, lookups int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests++
				w.WriteHeader(status)
			}))
			defer server.Close()

			endpoint := testProbeEndpoint(t, server, "")
			lookup := func(context.Context, string) ([]netip.Addr, error) {
				lookups++
				return testProbeLookup(), nil
			}
			latency, ok := checkDNSOne(context.Background(), lookup, endpoint)
			wantOK := status == http.StatusOK || status == http.StatusNoContent
			if ok != wantOK {
				t.Fatalf("checkDNSOne() ok = %t, want %t (endpoint %q, requests %d, lookups %d, latency %s)", ok, wantOK, endpoint, requests, lookups, latency)
			}
			if wantOK && latency <= 0 {
				t.Fatalf("checkDNSOne() latency = %s, want positive DNS lookup time", latency)
			}
		})
	}
}

func TestCheckDNSOneFollowsAtMostThreeRedirects(t *testing.T) {
	for _, redirects := range []int{3, 4} {
		t.Run(fmt.Sprintf("%d redirects", redirects), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				step := 0
				if _, err := fmt.Sscanf(r.URL.Path, "/%d", &step); err != nil {
					t.Errorf("bad redirect path %q: %v", r.URL.Path, err)
				}
				if step < redirects {
					http.Redirect(w, r, fmt.Sprintf("/%d", step+1), http.StatusFound)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()

			endpoint := testProbeEndpoint(t, server, "/0")
			lookup := func(context.Context, string) ([]netip.Addr, error) {
				return testProbeLookup(), nil
			}
			_, ok := checkDNSOne(context.Background(), lookup, endpoint)
			if wantOK := redirects == 3; ok != wantOK {
				t.Fatalf("checkDNSOne() ok = %t, want %t", ok, wantOK)
			}
		})
	}
}

func TestCheckDNSOneHonorsContextDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	endpoint := testProbeEndpoint(t, server, "")
	lookup := func(ctx context.Context, _ string) ([]netip.Addr, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, ok := checkDNSOne(ctx, lookup, endpoint); ok {
		t.Fatal("checkDNSOne() succeeded after its context deadline")
	}
}

func dnsAResponse(ip netip.Addr) []byte {
	query, err := buildDNSQuery("example.com")
	if err != nil {
		panic(err)
	}
	response := append([]byte(nil), query...)
	response[2], response[3] = 0x81, 0x80
	binary.BigEndian.PutUint16(response[6:8], 1)
	response = append(response, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 0, 0, 4)
	a := ip.As4()
	return append(response, a[:]...)
}

func testProbeEndpoint(t *testing.T, server *httptest.Server, path string) string {
	t.Helper()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("http://probe.test:%s%s", u.Port(), path)
}

func testProbeLookup() []netip.Addr {
	time.Sleep(time.Millisecond)
	return []netip.Addr{netip.MustParseAddr("127.0.0.1")}
}
