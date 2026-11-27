package xray

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net/netip"
	"strings"

	"github.com/xtls/xray-core/common/geodata"
	"github.com/xtls/xray-core/common/platform/filesystem"
	"google.golang.org/protobuf/proto"
)

// GeoIPCIDRs lists the CIDRs of one geoip.dat tag (e.g. "cloudflare"), one per line. It reads the
// file Xray routes with (see InitEnv), so a geo assets update changes them too. The file is
// streamed and only the wanted entry is decoded.
func GeoIPCIDRs(code string) (string, error) {
	r, _, err := filesystem.OpenAsset("geoip.dat")
	if err != nil {
		return "", err
	}
	defer r.Close()
	br := bufio.NewReaderSize(r, 64*1024)
	for {
		tag, err := binary.ReadUvarint(br) // GeoIPList.entry
		if err == io.EOF {
			return "", fmt.Errorf("geoip:%s not found in geoip.dat", code)
		}
		if err != nil || tag != 1<<3|2 {
			return "", fmt.Errorf("geoip.dat is malformed")
		}
		size, err := binary.ReadUvarint(br)
		if err != nil {
			return "", err
		}
		head, err := br.Peek(int(min(size, 64))) // GeoIP.code: tag, length, bytes
		if err != nil {
			return "", err
		}
		if len(head) > 2 && head[0] == 1<<3|2 && int(head[1]) <= len(head)-2 && strings.EqualFold(string(head[2:2+head[1]]), code) {
			body := make([]byte, size)
			if _, err := io.ReadFull(br, body); err != nil {
				return "", err
			}
			var entry geodata.GeoIP
			if err := proto.Unmarshal(body, &entry); err != nil {
				return "", err
			}
			var out strings.Builder
			for _, c := range entry.Cidr {
				if addr, ok := netip.AddrFromSlice(c.Ip); ok {
					fmt.Fprintln(&out, netip.PrefixFrom(addr, int(c.Prefix)))
				}
			}
			return out.String(), nil
		}
		if _, err := br.Discard(int(size)); err != nil {
			return "", err
		}
	}
}
