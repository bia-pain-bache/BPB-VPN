package scanner

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

type Protocol string

const (
	VLESS    Protocol = "vless"
	WARP     Protocol = "warp"
	MASQUEH2 Protocol = "masque-h2"
	MASQUEH3 Protocol = "masque-h3"
)

type Range struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

type Fragment struct {
	Packets  string  `json:"packets"`
	Lengths  []Range `json:"lengths"`
	Delays   []Range `json:"delays"`
	MaxSplit *Range  `json:"maxSplit"`
}

type Noise struct {
	Type   string `json:"type"`
	Size   Range  `json:"size"`
	Bytes  []int  `json:"bytes"`
	Packet string `json:"packet"`
	Count  int    `json:"count"`
	Delay  Range  `json:"delay"`
}

type Options struct {
	Protocol        Protocol   `json:"protocol"`
	Mode            string     `json:"mode"`
	SNI             string     `json:"sni"`
	Port            int        `json:"port"`
	CIDRs           []string   `json:"cidrs"`
	EnableIPv6      bool       `json:"enableIpv6"`
	ProbeCount      int        `json:"probeCount"`
	OutputCount     int        `json:"outputCount"`
	AttemptCount    int        `json:"attemptCount"`
	AttemptInterval int        `json:"attemptInterval"` // milliseconds
	ProbeInterval   int        `json:"probeInterval"`   // milliseconds
	Fragments       []Fragment `json:"fragments"`
	Noises          []Noise    `json:"noises"`
	NoiseReset      Range      `json:"noiseReset"`
}

func ParseOptions(raw string) (Options, error) {
	var options Options
	if err := json.Unmarshal([]byte(raw), &options); err != nil {
		return Options{}, fmt.Errorf("scanner options: %w", err)
	}
	return options.withDefaultsAndValidation()
}

func (o Options) withDefaultsAndValidation() (Options, error) {
	switch o.Protocol {
	case VLESS, WARP, MASQUEH2, MASQUEH3:
	default:
		return Options{}, fmt.Errorf("unsupported scanner protocol %q", o.Protocol)
	}
	if strings.TrimSpace(o.SNI) == "" {
		return Options{}, errors.New("SNI is required")
	}
	o.SNI = strings.TrimSpace(o.SNI)
	if o.Port == 0 {
		o.Port = 443
	}
	if o.Port < 1 || o.Port > 65535 {
		return Options{}, errors.New("port must be between 1 and 65535")
	}
	if o.Mode == "" {
		o.Mode = "normal"
	}
	switch {
	case (o.Protocol == MASQUEH3 || o.Protocol == WARP) && (o.Mode == "normal" || o.Mode == "noise"):
	case (o.Protocol == VLESS || o.Protocol == MASQUEH2) && (o.Mode == "normal" || o.Mode == "fragment"):
	default:
		return Options{}, fmt.Errorf("mode %q is not available for %s", o.Mode, o.Protocol)
	}
	if o.ProbeCount == 0 {
		o.ProbeCount = 100
	}
	if o.ProbeCount < 1 {
		return Options{}, errors.New("probe count must be positive")
	}
	if o.ProbeCount > 1_000_000 {
		return Options{}, errors.New("probe count cannot exceed 1000000")
	}
	if o.OutputCount == 0 {
		o.OutputCount = 10
	}
	if o.OutputCount < 1 {
		return Options{}, errors.New("output count must be positive")
	}
	if o.OutputCount > 10_000 {
		return Options{}, errors.New("output count cannot exceed 10000")
	}
	if o.AttemptCount == 0 {
		o.AttemptCount = 3
	}
	if o.AttemptCount < 1 || o.AttemptCount > 5 {
		return Options{}, errors.New("attempt count must be between 1 and 5")
	}
	if o.AttemptInterval == 0 {
		o.AttemptInterval = 50
	}
	if o.AttemptInterval < 10 || o.AttemptInterval > 100 {
		return Options{}, errors.New("attempt interval must be between 10 and 100 ms")
	}
	if o.ProbeInterval == 0 {
		o.ProbeInterval = 100
	}
	if o.ProbeInterval < 100 || o.ProbeInterval > 1000 {
		return Options{}, errors.New("probe interval must be between 100 and 1000 ms")
	}
	o.NoiseReset = defaultRange(o.NoiseReset, Range{Min: 30, Max: 60})

	cidrs := make([]string, 0, len(o.CIDRs))
	for _, value := range o.CIDRs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			return Options{}, fmt.Errorf("invalid CIDR %q: %w", value, err)
		}
		cidrs = append(cidrs, prefix.Masked().String())
	}
	if len(cidrs) == 0 {
		return Options{}, errors.New("at least one CIDR is required")
	}
	o.CIDRs = uniqueStrings(cidrs)
	if o.Mode == "fragment" {
		if len(o.Fragments) == 0 {
			return Options{}, errors.New("fragment mode requires at least one configured fragment")
		}
		for i, fragment := range o.Fragments {
			if err := validateFragment(fragment); err != nil {
				return Options{}, fmt.Errorf("fragment %d: %w", i+1, err)
			}
		}
	}
	if o.Mode == "noise" {
		if len(o.Noises) == 0 {
			return Options{}, errors.New("UDP-noise mode requires configured noises")
		}
		for i, noise := range o.Noises {
			if noise.Count < 1 || noise.Count > 100 {
				return Options{}, fmt.Errorf("noise %d count must be between 1 and 100", i+1)
			}
			if noise.Delay.Min < 0 || noise.Delay.Max < noise.Delay.Min {
				return Options{}, fmt.Errorf("noise %d delay range must be non-negative and ordered", i+1)
			}
			switch noise.Type {
			case "rand":
				if noise.Size.Min < 0 || noise.Size.Max < noise.Size.Min {
					return Options{}, fmt.Errorf("noise %d size range must be non-negative and ordered", i+1)
				}
			case "array":
				for _, value := range noise.Bytes {
					if value < 0 || value > 255 {
						return Options{}, fmt.Errorf("noise %d array bytes must be between 0 and 255", i+1)
					}
				}
			case "str", "hex", "base64", "exp":
			default:
				return Options{}, fmt.Errorf("noise %d has unsupported type %q", i+1, noise.Type)
			}
		}
	}
	return o, nil
}

func validateFragment(f Fragment) error {
	switch f.Packets {
	case "tlsHello", "1-1", "1-2", "1-3", "1-5":
	default:
		return fmt.Errorf("unsupported packet selection %q", f.Packets)
	}
	if len(f.Lengths) == 0 || len(f.Delays) == 0 {
		return errors.New("lengths and delays are required")
	}
	for _, r := range append(append([]Range{}, f.Lengths...), f.Delays...) {
		if r.Min < 0 || r.Max < r.Min {
			return errors.New("range values must be non-negative and ordered")
		}
	}
	if f.MaxSplit != nil && (f.MaxSplit.Min < 0 || f.MaxSplit.Max < f.MaxSplit.Min) {
		return errors.New("maxSplit range must be non-negative and ordered")
	}
	return nil
}

func defaultRange(value, fallback Range) Range {
	if value.Min == 0 && value.Max == 0 {
		return fallback
	}
	return value
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
