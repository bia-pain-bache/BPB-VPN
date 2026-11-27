package xray

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/xtls/xray-core/common/geodata"
	"google.golang.org/protobuf/proto"
)

func cidr(ip string, prefix uint32) *geodata.CIDR {
	return &geodata.CIDR{Ip: net.ParseIP(ip).To4(), Prefix: prefix}
}

func TestGeoIPCIDRs(t *testing.T) {
	v6 := &geodata.CIDR{Ip: net.ParseIP("2606:4700::"), Prefix: 32}
	raw, err := proto.Marshal(&geodata.GeoIPList{Entry: []*geodata.GeoIP{
		{Code: "IR", Cidr: []*geodata.CIDR{cidr("5.22.0.0", 16)}},
		{Code: "CLOUDFLARE", Cidr: []*geodata.CIDR{cidr("104.16.0.0", 13), v6}},
		{Code: "FASTLY", Cidr: []*geodata.CIDR{cidr("151.101.0.0", 16)}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "geoip.dat"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envAsset, dir)

	got, err := GeoIPCIDRs("cloudflare")
	if err != nil {
		t.Fatal(err)
	}
	if want := "104.16.0.0/13\n2606:4700::/32\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if _, err := GeoIPCIDRs("warp"); err == nil {
		t.Fatal("a tag missing from geoip.dat must be an error")
	}
}
