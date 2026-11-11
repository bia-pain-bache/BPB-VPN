# BPB VPN is here

Welcome to BPB VPN, a companion app designed to make setting up and using the BPB Panel and Wizard simpler and more approachable. From getting started to managing your VPN connection and even combining it with third-party proxies like Tor and Psiphon, my goal is to make the experience easier to follow and less of a hassle.

## BPB Cloudflare scanners

BPB is now equipped with specific Cloudflare scanners for all protocol! The scanner follows user settings for protocol i.e. Fingerprint, SNI, IP version, Custom domain, Transport and is even capable of scanning with Fragment and UDP Obfuscation (ECH is excluded). Please note that the more `Probes count` you set, the better IPs you get, but the downside is the scanner consumes more worker requests during VLESS scanning.

## Bug fixes and Improvements

- Added **Snowflake AMP** to Tor modes.
- Added **Routing Strategy**.
- **Connection behaviour:** The app now checks the actual before updating the UI. As a result, connections that are not working remain in the `Connecting` state, unlike the usual behavior in v2rayNG and similar apps.
- Renamed **Serverless Fragment** to **Serverless Packet**, since it also supports UDP obfuscation.
- Updated the default **UDP obfuscation** noises to simulate the AmneziaWG obfuscation.
- Added **QUIC**, **DNS query**, **STUN Binding** mimic presets to UDP noises, along with a **None** mode to apply custom packet for **Expression** UDP noise mode.
- Changed the default Warp **Reserved bytes** value to disabled to reduce protocol fingerprinting on some ISPs.
- Added bilingual Github issue templates in Farsi and English. It worth mentioning that all BPB projects are handled by one person, so please only report technical bugs and misconfigurations (not the app performance on your ISP) with all requested info, issue gets closed ASAP otherwise.
- Updated **Reset settings** behaviour so that clean IPs and endpoints are preserved.
- Updated the **Domain Fronting** + **Psiphon** config, it has much better performance now and tries to keep @patterniha template safe.
- Updated the routing rules to block Iran firewall IPs by default.
- Fixed the BPB quick settings tile bug #9

![GitHub Downloads (all assets, specific tag)](https://img.shields.io/github/downloads/bia-pain-bache/BPB-VPN/v1.1.0/total)
