package scanner

import (
	"context"
	"net/netip"
	"strings"
	"testing"
)

func TestSelectScanIPsAddressFamilies(t *testing.T) {
	tests := []struct {
		name       string
		cidrs      []string
		count      int
		enableIPv6 bool
		wantV4     int
		wantV6     int
		wantError  string
	}{
		{"ipv6 disabled", []string{"192.0.2.0/24", "2000::/3"}, 100, false, 100, 0, ""},
		{"balanced", []string{"192.0.2.0/24", "2000::/3"}, 100, true, 50, 50, ""},
		{"odd count", []string{"192.0.2.0/24", "2000::/3"}, 5, true, 3, 2, ""},
		{"ipv4 shortage", []string{"192.0.2.1/32", "2000::/3"}, 5, true, 1, 4, ""},
		{"ipv6 shortage", []string{"192.0.2.0/24", "2001:db8::1/128"}, 5, true, 4, 1, ""},
		{"ipv4 only", []string{"192.0.2.0/24"}, 5, true, 5, 0, ""},
		{"ipv6 only", []string{"2000::/3"}, 5, true, 0, 5, ""},
		{"ipv6 disabled no ipv4", []string{"2000::/3"}, 5, false, 0, 0, "no IPv4"},
		{"insufficient addresses", []string{"192.0.2.1/32", "2001:db8::1/128"}, 3, true, 0, 0, "at most 2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ips, err := SelectScanIPs(context.Background(), tt.cidrs, tt.count, tt.enableIPv6)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("error = %v, want %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			seen := make(map[string]bool)
			v4, v6 := 0, 0
			for _, ip := range ips {
				if seen[ip] {
					t.Fatalf("duplicate IP %s", ip)
				}
				seen[ip] = true
				addr := netip.MustParseAddr(ip)
				if addr.Is4() {
					v4++
				} else {
					v6++
				}
			}
			if v4 != tt.wantV4 || v6 != tt.wantV6 {
				t.Fatalf("got %d IPv4 and %d IPv6, want %d and %d", v4, v6, tt.wantV4, tt.wantV6)
			}
		})
	}
}
