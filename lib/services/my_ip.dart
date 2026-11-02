import 'dart:convert';
import 'dart:io';

import 'package:http/http.dart' as http;
import 'package:http/io_client.dart';

import 'local_proxy.dart';

/// An exit IP and its two-letter country code (if known).
typedef IpInfo = ({String ip, String? country});

/// The two exit IPs the BPB panel shows: one for sites behind Cloudflare
/// (reached through the worker's proxy IP) and one for all other sites.
class MyIps {
  final IpInfo? cloudflare;
  final IpInfo? other;
  const MyIps({this.cloudflare, this.other});
}

/// Looks up both IPs through [proxyPort] (the tunnel's local proxy); the app's own
/// traffic is excluded from the VPN, so a direct request would show the real IP.
Future<MyIps> fetchMyIps({required int proxyPort}) async {
  final client = proxiedClient(proxyPort);
  final nocache = DateTime.now().millisecondsSinceEpoch;

  Future<String?> get(String url) async {
    try {
      final res = await client
          .get(Uri.parse(url))
          .timeout(const Duration(seconds: 20));
      return res.statusCode == 200 ? res.body : null;
    } catch (_) {
      return null;
    }
  }

  // String? validIp(String? s) =>
  //     s != null && InternetAddress.tryParse(s.trim()) != null ? s.trim() : null;

  Future<IpInfo?> cloudflareTargets() async {
    // api.ip.sb is hosted on Cloudflare: the worker connects to it through its proxy IP.
    final body = await get('https://api-ipv4.ip.sb/geoip&nocache=$nocache');
    if (body == null) return null;

    try {
      final data = jsonDecode(body) as Map<String, dynamic>;
      return (
        ip: data['ip'] as String,
        country: data['continent_code'] as String,
      );
    } catch (_) {
      return null;
    }
  }

  Future<IpInfo?> otherTargets() async {
    // ip-api is not on Cloudflare: the worker connects to it directly.
    final body = await get(
      'http://ip-api.com/json/?fields=query,countryCode&nocache=$nocache',
    );
    if (body == null) return null;

    try {
      final data = jsonDecode(body) as Map<String, dynamic>;
      return (
        ip: data['query'] as String,
        country: data['countryCode'] as String,
      );
    } catch (_) {
      return null;
    }
  }

  try {
    final results = await Future.wait([cloudflareTargets(), otherTargets()]);
    return MyIps(cloudflare: results[0], other: results[1]);
  } finally {
    client.close();
  }
}

/// "DE" → 🇩🇪 (regional indicator symbols).
String countryFlag(String code) => String.fromCharCodes(
  code.toUpperCase().codeUnits.map((c) => 0x1F1E6 + c - 0x41),
);

/// HTTP client that goes through the tunnel when [proxyPort] is set.
http.Client proxiedClient(int? proxyPort) {
  final inner = HttpClient()..connectionTimeout = const Duration(seconds: 15);
  if (proxyPort != null) LocalProxy.apply(inner, port: proxyPort);
  return IOClient(inner);
}

/// This phone's IPv4 address on a local network (Wi-Fi, hotspot, USB tethering), for
/// devices using its proxy over LAN. Null when it's on none (e.g. mobile data only).
Future<String?> lanIPv4() async {
  // Lower is preferred; mobile data (rmnet/ccmni) and the VPN's own TUN aren't LANs.
  int? rank(String name) => switch (name) {
    _ when name.startsWith('wlan') => 0, // Wi-Fi
    _ when name.startsWith('swlan') || name.startsWith('ap') => 1, // hotspot
    _
        when name.startsWith('rndis') ||
            name.startsWith('usb') ||
            name.startsWith('eth') =>
      2, // tethering
    _ => null,
  };
  try {
    final found = <(int, String)>[
      for (final i in await NetworkInterface.list(
        type: InternetAddressType.IPv4,
      ))
        if (rank(i.name) case final r?)
          for (final a in i.addresses)
            if (!a.isLoopback && !a.isLinkLocal) (r, a.address),
    ]..sort((a, b) => a.$1.compareTo(b.$1));
    return found.firstOrNull?.$2;
  } catch (_) {
    return null;
  }
}
