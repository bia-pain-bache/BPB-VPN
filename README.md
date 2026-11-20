# BPB VPN

**One app. Multiple ways to stay connected.**

BPB brings highly customizable Cloudflare CDN connections, Psiphon, Tor, SSH and Serverless solutions together in one approachable VPN client powered by [Xray-core](https://xtls.github.io). Start with the guided setup, then choose a ready-to-use connection or fine-tune the details for your network.

> Built for networks where ordinary connections may be restricted. Results depend on your network and configuration; no method can guarantee access everywhere.

## What makes BPB different?

- **Your own Cloudflare setup:** Deploy private Workers and Pages from the app using your own Cloudflare account. The guided wizard walks you through it.
- **More than one route:** Choose VLESS, WARP, MASQUE, SSH or Serverless connections. Psiphon and Tor are also optimized, built in and can be combined with Serverless methods to bypass regular restrictions.
- **Tunable for your network:** Adjust SNI and connection IPs, TLS fingerprints, TCP fragmentation, UDP obfuscation, DNS, routing and more DPI bypassing tricks without being locked into one preset.
- **Find responsive endpoints:** Built-in **REAL** scanners help discover Cloudflare IPs and WARP/MASQUE endpoints. Results depend on your current network and selected settings.
- **Useful everyday controls:** Per-app proxying, proxy-chain imports, connection logs, configuration sharing and customizable theme.

## Get started

1. Download the latest Android APK from [Releases](https://github.com/bia-pain-bache/BPB-VPN/releases/latest).
2. Open BPB and follow the setup wizard. BPB guides deployment in the app.
3. Choose a connection and connect. Psiphon, Tor and Serverless are available without adding a Cloudflare account.

Use the **universal APK** if you are unsure which download to choose. Release notes and checksums are included with each release.

## Customize when you need to

Most users can begin with the defaults. If a connection needs adjustment, BPB brings the relevant controls together:

- Choose or scan clean IPs, endpoints, Tor bridges, and tune SNI and TLS fingerprint settings.
- Enable TCP fragmentation or UDP obfuscation where they help your network.
- Configure traffic splitting (routing), DNS, IPv4/IPv6, or proxy only selected apps.
- Import supported v2ray links or WireGuard configurations as chain proxies.
- Review live connection logs and export or share settings with others.

These are options to experiment with no guarantee. The best settings vary by ISP, location and network conditions.

## Platform

The VPN is only supported on **Android** for now. Other platform builds will be released one by one.

## Open source

BPB is licensed under the [GNU GPL v3.0](LICENSE). See [release notes](RELEASE_NOTES.md) for recent changes and [Issues](https://github.com/bia-pain-bache/BPB-VPN/issues) to report technical bugs or misconfigurations.
