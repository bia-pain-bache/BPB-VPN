package scanner

import (
	"strings"
	"testing"
)

func TestParseOptionsAppliesScannerDefaults(t *testing.T) {
	options, err := ParseOptions(`{"protocol":"vless","sni":"example.com","cidrs":["192.0.2.0/24"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if options.Mode != "normal" || options.Port != 443 || options.ProbeCount != 100 ||
		options.OutputCount != 10 || options.AttemptCount != 3 ||
		options.AttemptInterval != 50 || options.ProbeInterval != 100 {
		t.Fatalf("unexpected defaults: %+v", options)
	}
}

func TestParseOptionsRejectsInvalidLimitsAndModes(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{"attempts", `{"protocol":"vless","sni":"example.com","cidrs":["192.0.2.0/24"],"attemptCount":6}`, "attempt count"},
		{"attempt too short", `{"protocol":"vless","sni":"example.com","cidrs":["192.0.2.0/24"],"attemptInterval":9}`, "attempt interval"},
		{"attempt too long", `{"protocol":"vless","sni":"example.com","cidrs":["192.0.2.0/24"],"attemptInterval":101}`, "attempt interval"},
		{"probe too short", `{"protocol":"vless","sni":"example.com","cidrs":["192.0.2.0/24"],"probeInterval":99}`, "probe interval"},
		{"probe too long", `{"protocol":"vless","sni":"example.com","cidrs":["192.0.2.0/24"],"probeInterval":1001}`, "probe interval"},
		{"h3 fragment", `{"protocol":"masque-h3","mode":"fragment","sni":"example.com","cidrs":["192.0.2.0/24"]}`, "mode"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseOptions(test.json)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want text %q", err, test.want)
			}
		})
	}
}

func TestParseOptionsAcceptsIntervalBoundaries(t *testing.T) {
	for _, raw := range []string{
		`{"protocol":"vless","sni":"example.com","cidrs":["192.0.2.0/24"],"attemptInterval":10,"probeInterval":100}`,
		`{"protocol":"vless","sni":"example.com","cidrs":["192.0.2.0/24"],"attemptInterval":100,"probeInterval":1000}`,
	} {
		if _, err := ParseOptions(raw); err != nil {
			t.Fatalf("valid interval limits rejected: %v", err)
		}
	}
}

func TestParseOptionsReadsScanSettings(t *testing.T) {
	options, err := ParseOptions(`{"protocol":"vless","sni":"example.com","cidrs":["192.0.2.0/24"],"probeCount":42,"probeInterval":180,"attemptCount":4,"attemptInterval":75}`)
	if err != nil {
		t.Fatal(err)
	}
	if options.ProbeCount != 42 || options.ProbeInterval != 180 || options.AttemptCount != 4 || options.AttemptInterval != 75 {
		t.Fatalf("unexpected scan settings: %+v", options)
	}
}

func TestParseOptionsAcceptsExpressionNoise(t *testing.T) {
	options, err := ParseOptions(`{
		"protocol":"masque-h3",
		"mode":"noise",
		"sni":"example.com",
		"cidrs":["192.0.2.0/24"],
		"noises":[{"type":"exp","packet":"<b 0xc3><r 8>","count":1}]
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if options.Noises[0].Type != "exp" {
		t.Fatalf("noise type = %q, want exp", options.Noises[0].Type)
	}
}
