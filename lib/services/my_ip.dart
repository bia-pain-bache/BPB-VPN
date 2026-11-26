import 'dart:convert';
import 'dart:io';

import 'package:bpb/services/vpn_channel.dart';
import 'package:http/http.dart' as http;
import 'package:http/io_client.dart';

import 'local_proxy.dart';

/// The two exit IPs the BPB panel shows: one for sites behind Cloudflare
/// (reached through the worker's proxy IP) and one for all other sites.
/// When disconnected, [local] holds the phone's real ISP IP/ISP.
class MyIps {
  final ({String ip, String? isp})? cloudflare;
  final ({String ip, String? isp})? other;
  final ({String ip, String? isp})? local;
  const MyIps({this.cloudflare, this.other, this.local});

  /// True when connected (has tunnel exit IPs).
  bool get isConnected => cloudflare != null || other != null;

  /// True when disconnected but we have a local ISP IP.
  bool get hasLocal => local != null;
}

/// Fetches exit IPs through the tunnel when connected, and always fetches the local ISP IP.
Future<MyIps> fetchMyIps({required bool connected}) async {
  final local = await fetchMyIspIp();

  if (!connected) {
    return MyIps(local: local);
  }

  final nocache = DateTime.now().millisecondsSinceEpoch.toString();
  final uri = Uri.https('ipv4.geojs.io', '/v1/ip/geo.json', {
    'nocache': nocache,
  }).toString();
  final uriCf = Uri.https('ifconfig.io', '/all.json', {'nocache': nocache}).toString();
  try {
    final results = await Future.wait([
      VpnChannel.getMyIp(uriCf),
      VpnChannel.getMyIp(uri),
    ]);
    return MyIps(cloudflare: results[0], other: results[1], local: local);
  } catch (_) {
    return MyIps(local: local);
  }
}

/// Local ISP IP
Future<({String ip, String? isp})?> fetchMyIspIp() async {
  try {
    final uri = Uri.https('ipv4.geojs.io', '/v1/ip/geo.json', {
      'nocache': DateTime.now().millisecondsSinceEpoch.toString(),
    });

    final response = await http.get(uri).timeout(const Duration(seconds: 10));
    if (response.statusCode != 200) return null;
    final data = jsonDecode(response.body);

    return (
      ip: data['ip'] as String,
      isp: data['organization_name'] as String?,
    );
  } catch (_) {
    return null;
  }
}

/// "DE" → 🇩🇪 (regional indicator symbols).
String countryFlag(String code) => String.fromCharCodes(
  code.toUpperCase().codeUnits.map((c) => 0x1F1E6 + c - 0x41),
);

/// HTTP client that goes through the tunnel when [proxyPort] is set.
http.Client proxiedClient(
  int? proxyPort, {
  Duration connectTimeout = const Duration(seconds: 15),
}) {
  final inner = HttpClient()..connectionTimeout = connectTimeout;
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
