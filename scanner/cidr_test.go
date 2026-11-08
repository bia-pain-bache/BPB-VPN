package scanner

import (
	"context"
	"errors"
	"io"
	"math/big"
	"net/netip"
	"testing"
)

func TestSelectIPsSupportsIPv4IPv6AndDeduplicates(t *testing.T) {
	offsets := []int64{0, 1, 2, 3}
	randomInt := func(_ io.Reader, max *big.Int) (*big.Int, error) {
		value := offsets[0]
		offsets = offsets[1:]
		if big.NewInt(value).Cmp(max) >= 0 {
			t.Fatalf("test offset %d outside bound %s", value, max)
		}
		return big.NewInt(value), nil
	}
	got, err := selectIPs(context.Background(), []string{"192.0.2.0/31", "2001:db8::/127"}, 4, randomInt)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"192.0.2.0", "192.0.2.1", "2001:db8::", "2001:db8::1"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("address %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSelectIPsDoesNotEnumerateLargeRange(t *testing.T) {
	got, err := SelectIPs(context.Background(), []string{"2000::/3"}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d addresses, want 3", len(got))
	}
	for _, value := range got {
		address, err := netip.ParseAddr(value)
		if err != nil || !netip.MustParsePrefix("2000::/3").Contains(address) {
			t.Fatalf("%q is outside the requested CIDR", value)
		}
	}
}

func TestSelectIPsRejectsInsufficientPool(t *testing.T) {
	_, err := selectIPs(context.Background(), []string{"192.0.2.1/32"}, 2, func(io.Reader, *big.Int) (*big.Int, error) {
		return big.NewInt(0), nil
	})
	if err == nil {
		t.Fatal("expected an insufficient-address error")
	}
}

func TestSelectIPsHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := SelectIPs(ctx, []string{"192.0.2.0/24"}, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestSelectIPsDeduplicatesOverlappingCIDRs(t *testing.T) {
	offsets := []int64{0, 1, 2}
	randomInt := func(_ io.Reader, max *big.Int) (*big.Int, error) {
		value := offsets[0]
		offsets = offsets[1:]
		if big.NewInt(value).Cmp(max) >= 0 {
			t.Fatalf("test offset %d outside bound %s", value, max)
		}
		return big.NewInt(value), nil
	}
	got, err := selectIPs(context.Background(), []string{"192.0.2.0/30", "192.0.2.2/31"}, 3, randomInt)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"192.0.2.0", "192.0.2.1", "192.0.2.2"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("address %d = %q, want %q", i, got[i], want[i])
		}
	}
}
