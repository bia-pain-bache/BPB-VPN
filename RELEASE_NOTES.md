# BPB VPN v1.3.0

This release introduces some important improvements like VLESS DNS compatibility and improvements, fallback DNS servers while using Exit Nodes and also supporting **SSH** connection for users who are not familiar with complicated VPS setups.

## 🌟 SSH protocol

Along with providing free private solutions for a wide range of users, I added this protocol to empower amateur users who have no knowledge of server configurations. It allows them to simply purchase a VPS in an emergency and connect via SSH with zero setup.  
Right now SSH proxies endure more than some modern protocols like reality on some ISPs in Iran for low bandwidth connections.  
It should be mentioned that SSH protocol only supports TCP traffic and you won't be able to use Telegram calls, Clubhouse etc.

## 🌟 Global Remote DNS

Merged all protocols' **Remote DNS** servers into one field in Global settings. VLESS now supports Cloudflare DNS servers and hijacks them into related DoH servers within worker.  
The Remote DNS only accepts IPv4 and IPv6 now to boost up performance specially for **VLESS**.

> [!IMPORTANT]
> The worker scripts get updated in the background after you update to this version. however it needs to connect to the internet, You can confirm worker update by checking `Settings` > `VLESS` > `Cloudflare accounts`, there will be a manual update button in case the update was not successful.

## 💡 Bug fixes and improvements

- Added SSH protocol as it needs zero configuration skills for amateur users, supporting password, SSH key methods and Keep Alive settings.
- Enabled Psiphon, Tor and Chain proxy (except WireGuard and Hysteria2) for SSH connection.
- Made Remote DNS global, improved VLESS DNS performance and added Cloudflare DNS support to.
- Added Tor and Psiphon DNS servers as a fallback to Remote DNS.
- Updated worker minification setup.
- Fixed scanner pause bug.
- Refactored the core logic.
- Fixed Tor and Psiphon routing bug in 1.2.2.

**Full Changelog**: https://github.com/bia-pain-bache/BPB-VPN/compare/v1.2.1...v1.3.0

![GitHub Downloads (all assets, specific tag)](https://img.shields.io/github/downloads/bia-pain-bache/BPB-VPN/v1.3.0/total)
