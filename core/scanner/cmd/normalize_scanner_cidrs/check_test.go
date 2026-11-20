package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeAsset(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckAssetsValidatesAllFilesWithoutModifyingThem(t *testing.T) {
	dir := t.TempDir()
	vless := "# manually maintained\n192.0.2.1/24\n2001:db8::/32\n"
	writeAsset(t, dir, "vless-cidrs.txt", vless)
	writeAsset(t, dir, "warp-cidrs.txt", "198.51.100.0/24\n")
	writeAsset(t, dir, "extra.txt", "203.0.113.0/24\n")

	if err := checkAssets(dir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "vless-cidrs.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != vless {
		t.Fatal("check modified the source file")
	}
	writeAsset(t, dir, "extra.txt", "not a CIDR\n")
	if err := checkAssets(dir); err == nil || !strings.Contains(err.Error(), "extra.txt") {
		t.Fatalf("invalid extra file: %v", err)
	}
}

func TestCheckAssetsRejectsInvalidRuntimeInputs(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
	}{
		{"bare IP", "192.0.2.1\n", "line 1"},
		{"inline comment", "192.0.2.0/24 # comment\n", "line 1"},
		{"mapped IPv6", "::ffff:192.0.2.1/128\n", "line 1"},
		{"empty", " \n# comment\n", "empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeAsset(t, dir, "vless-cidrs.txt", "192.0.2.0/24\n")
			writeAsset(t, dir, "warp-cidrs.txt", tc.input)
			if err := checkAssets(dir); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestCheckAssetsRequiresBothBundledFiles(t *testing.T) {
	dir := t.TempDir()
	writeAsset(t, dir, "vless-cidrs.txt", "192.0.2.0/24\n")
	if err := checkAssets(dir); err == nil || !strings.Contains(err.Error(), "warp-cidrs.txt") {
		t.Fatalf("missing asset error = %v", err)
	}
}

func TestNormalizeStillAcceptsBareIPsAndInlineComments(t *testing.T) {
	dir := t.TempDir()
	writeAsset(t, dir, "source.txt", "192.0.2.1 # host\n2001:db8::1\n192.0.2.1/32\n")
	target := filepath.Join(dir, "output.txt")
	if err := normalize(filepath.Join(dir, "source.txt"), target); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "192.0.2.1/32\n2001:db8::1/128\n" {
		t.Fatalf("unexpected normalized output %q", got)
	}
}
