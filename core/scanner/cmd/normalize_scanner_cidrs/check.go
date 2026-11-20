package main

import (
	"bufio"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
)

func checkAssets(directory string) error {
	for _, name := range []string{"vless-cidrs.txt", "warp-cidrs.txt"} {
		info, err := os.Lstat(filepath.Join(directory, name))
		if err != nil {
			return fmt.Errorf("required CIDR asset %s: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("required CIDR asset %s is not a regular file", name)
		}
	}
	return filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("read CIDR assets: %w", err)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("CIDR asset %s is not a regular file", path)
		}
		return checkAsset(path)
	})
}

func checkAsset(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open CIDR asset %s: %w", path, err)
	}
	defer file.Close()

	count := 0
	scanner := bufio.NewScanner(file)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		prefix, err := netip.ParsePrefix(line)
		if err != nil {
			return fmt.Errorf("CIDR asset %s line %d: %w", path, lineNumber, err)
		}
		if prefix.Addr().Is4In6() {
			return fmt.Errorf("CIDR asset %s line %d: IPv4-mapped CIDRs are not supported", path, lineNumber)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read CIDR asset %s: %w", path, err)
	}
	if count == 0 {
		return fmt.Errorf("CIDR asset %s is empty", path)
	}
	fmt.Printf("Validated %d CIDRs in %s\n", count, path)
	return nil
}
