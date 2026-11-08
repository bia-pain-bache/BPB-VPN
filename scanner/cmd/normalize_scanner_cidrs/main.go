package main

import (
	"bufio"
	"fmt"
	"net/netip"
	"os"
	"sort"
	"strings"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: normalize_scanner_cidrs <input> <output> | --check <asset-directory>")
		os.Exit(2)
	}
	var err error
	if os.Args[1] == "--check" {
		err = checkAssets(os.Args[2])
	} else {
		err = normalize(os.Args[1], os.Args[2])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func normalize(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open CIDR list: %w", err)
	}
	defer input.Close()

	unique := make(map[string]netip.Prefix)
	scanner := bufio.NewScanner(input)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line, _, _ := strings.Cut(scanner.Text(), "#")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		prefix, err := netip.ParsePrefix(line)
		if err != nil {
			address, addressErr := netip.ParseAddr(line)
			if addressErr != nil {
				return fmt.Errorf("invalid CIDR asset line %d: %w", lineNumber, err)
			}
			prefix = netip.PrefixFrom(address, address.BitLen())
		}
		if !prefix.IsValid() || prefix.Addr().Is4In6() {
			return fmt.Errorf("invalid CIDR asset line %d: unsupported address %q", lineNumber, line)
		}
		prefix = prefix.Masked()
		unique[prefix.String()] = prefix
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read downloaded CIDR list: %w", err)
	}
	if len(unique) == 0 {
		return fmt.Errorf("CIDR asset is empty")
	}

	prefixes := make([]netip.Prefix, 0, len(unique))
	for _, prefix := range unique {
		prefixes = append(prefixes, prefix)
	}
	sort.Slice(prefixes, func(i, j int) bool {
		left, right := prefixes[i], prefixes[j]
		if left.Addr().Is4() != right.Addr().Is4() {
			return left.Addr().Is4()
		}
		if order := left.Addr().Compare(right.Addr()); order != 0 {
			return order < 0
		}
		return left.Bits() < right.Bits()
	})

	var output strings.Builder
	for _, prefix := range prefixes {
		output.WriteString(prefix.String())
		output.WriteByte('\n')
	}
	if err := os.WriteFile(target, []byte(output.String()), 0o644); err != nil {
		return fmt.Errorf("write normalized CIDR list: %w", err)
	}
	fmt.Printf("Validated %d CIDRs\n", len(prefixes))
	return nil
}
