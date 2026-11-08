package scanner

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/netip"
	"sort"
	"strings"
)

var errEmptyAddressPool = errors.New("CIDR selection contains no addresses")

type addressRange struct {
	bits  int
	start *big.Int
	size  *big.Int
}

func parseRanges(cidrs []string) ([]addressRange, *big.Int, error) {
	ranges := make([]addressRange, 0, len(cidrs))
	for _, cidr := range cidrs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(cidr))
		if err != nil {
			return nil, nil, fmt.Errorf("invalid CIDR %q: %w", cidr, err)
		}
		prefix = prefix.Masked()
		if prefix.Addr().Is4In6() {
			return nil, nil, fmt.Errorf("IPv4-mapped CIDR %q is not supported", cidr)
		}
		bits := prefix.Addr().BitLen()
		size := new(big.Int).Lsh(big.NewInt(1), uint(bits-prefix.Bits()))
		start := addressToBig(prefix.Addr())
		ranges = append(ranges, addressRange{bits: bits, start: start, size: size})
	}
	if len(ranges) == 0 {
		return nil, nil, errEmptyAddressPool
	}
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].bits != ranges[j].bits {
			return ranges[i].bits < ranges[j].bits
		}
		return ranges[i].start.Cmp(ranges[j].start) < 0
	})
	union := make([]addressRange, 0, len(ranges))
	for _, current := range ranges {
		if len(union) == 0 || union[len(union)-1].bits != current.bits {
			union = append(union, current)
			continue
		}
		last := &union[len(union)-1]
		lastEnd := new(big.Int).Sub(new(big.Int).Add(last.start, last.size), big.NewInt(1))
		if current.start.Cmp(new(big.Int).Add(lastEnd, big.NewInt(1))) > 0 {
			union = append(union, current)
			continue
		}
		currentEnd := new(big.Int).Sub(new(big.Int).Add(current.start, current.size), big.NewInt(1))
		if currentEnd.Cmp(lastEnd) > 0 {
			last.size = new(big.Int).Sub(new(big.Int).Add(currentEnd, big.NewInt(1)), last.start)
		}
	}
	total := new(big.Int)
	for _, current := range union {
		total.Add(total, current.size)
	}
	return union, total, nil
}

func SelectIPs(ctx context.Context, cidrs []string, count int) ([]string, error) {
	return selectIPs(ctx, cidrs, count, rand.Int)
}

// SelectScanIPs balances address families instead of weighting them by pool size.
func SelectScanIPs(ctx context.Context, cidrs []string, count int, enableIPv6 bool) ([]string, error) {
	var ipv4, ipv6 []string
	for _, cidr := range cidrs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(cidr))
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q: %w", cidr, err)
		}
		if prefix.Addr().Is4In6() {
			return nil, fmt.Errorf("IPv4-mapped CIDR %q is not supported", cidr)
		}
		if prefix.Addr().Is4() {
			ipv4 = append(ipv4, cidr)
		} else if enableIPv6 {
			ipv6 = append(ipv6, cidr)
		}
	}
	if len(ipv4) == 0 && len(ipv6) == 0 {
		if !enableIPv6 && len(cidrs) > 0 {
			return nil, errors.New("selected CIDRs contain no IPv4 addresses; enable IPv6 or select an IPv4 CIDR")
		}
		return nil, errEmptyAddressPool
	}
	if len(ipv4) == 0 {
		return SelectIPs(ctx, ipv6, count)
	}
	if len(ipv6) == 0 {
		return SelectIPs(ctx, ipv4, count)
	}
	if count < 0 {
		return nil, errors.New("count cannot be negative")
	}
	_, v4Capacity, err := parseRanges(ipv4)
	if err != nil {
		return nil, err
	}
	_, v6Capacity, err := parseRanges(ipv6)
	if err != nil {
		return nil, err
	}
	if new(big.Int).Add(v4Capacity, v6Capacity).Cmp(big.NewInt(int64(count))) < 0 {
		return nil, fmt.Errorf("requested %d IPs but the selected CIDRs contain at most %s addresses", count, new(big.Int).Add(v4Capacity, v6Capacity))
	}
	v4Count := (count + 1) / 2
	v6Count := count - v4Count
	if v4Capacity.Cmp(big.NewInt(int64(v4Count))) < 0 {
		v4Count = int(v4Capacity.Int64())
		v6Count = count - v4Count
	} else if v6Capacity.Cmp(big.NewInt(int64(v6Count))) < 0 {
		v6Count = int(v6Capacity.Int64())
		v4Count = count - v6Count
	}
	v4, err := SelectIPs(ctx, ipv4, v4Count)
	if err != nil {
		return nil, err
	}
	v6, err := SelectIPs(ctx, ipv6, v6Count)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, count)
	for i := 0; i < max(len(v4), len(v6)); i++ {
		if i < len(v4) {
			result = append(result, v4[i])
		}
		if i < len(v6) {
			result = append(result, v6[i])
		}
	}
	return result, nil
}

func selectIPs(ctx context.Context, cidrs []string, count int, randomInt func(io.Reader, *big.Int) (*big.Int, error)) ([]string, error) {
	if count < 0 {
		return nil, errors.New("count cannot be negative")
	}
	ranges, total, err := parseRanges(cidrs)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return []string{}, nil
	}
	if total.IsInt64() && int64(count) > total.Int64() {
		return nil, fmt.Errorf("requested %d IPs but the selected CIDRs contain at most %s addresses", count, total)
	}
	selected := make(map[string]struct{}, count)
	result := make([]string, 0, count)
	for len(result) < count {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		offset, err := randomInt(rand.Reader, total)
		if err != nil {
			return nil, fmt.Errorf("choose random IP offset: %w", err)
		}
		address := addressAt(ranges, offset)
		key := address.String()
		if _, exists := selected[key]; exists {
			continue
		}
		selected[key] = struct{}{}
		result = append(result, key)
	}
	return result, nil
}

func addressAt(ranges []addressRange, offset *big.Int) netip.Addr {
	remaining := new(big.Int).Set(offset)
	for _, r := range ranges {
		if remaining.Cmp(r.size) < 0 {
			value := new(big.Int).Add(r.start, remaining)
			return bigToAddress(value, r.bits)
		}
		remaining.Sub(remaining, r.size)
	}
	return netip.Addr{}
}

func addressToBig(address netip.Addr) *big.Int {
	bytes := address.AsSlice()
	return new(big.Int).SetBytes(bytes)
}

func bigToAddress(value *big.Int, bits int) netip.Addr {
	if bits == 32 {
		var bytes [4]byte
		value.FillBytes(bytes[:])
		return netip.AddrFrom4(bytes)
	}
	var bytes [16]byte
	value.FillBytes(bytes[:])
	return netip.AddrFrom16(bytes)
}
