import 'dart:convert';
import 'dart:io';

import 'package:bpb/models/settings.dart';
import 'package:bpb/services/der.dart';
import 'package:bpb/services/masque.dart';
import 'package:bpb/services/xray_config.dart';
import 'package:flutter_test/flutter_test.dart';

const account = {
  'privateKey': 'cHJpdmF0ZQ==',
  'publicKey': '-----BEGIN PUBLIC KEY-----\nMFkw\n-----END PUBLIC KEY-----\n',
  'ipv4': '172.16.0.2',
  'ipv6': '2606:4700:110:1::2',
};

Map<String, dynamic> masque([Map<String, dynamic> extra = const {}]) => {
  'masque': {
    'account': account,
    'endpoints': ['162.159.198.2:443', '[2606:4700:103::2]:8443'],
    ...extra,
  },
};

Future<XrayBuild> build(
  String variant, [
  Map<String, dynamic> profile = const {},
  Map<String, dynamic> device = const {},
]) => XrayConfigBuilder(
  Profile.fromJson({...masque(), ...profile}),
  DeviceSettings.fromJson({
    'connection': {'masqueVariant': variant},
    ...device,
  }),
  resolver: (host, {required bool ipv6}) async => const [],
).build(ConnectionMode.masque);

List<Map> proxies(Map c) =>
    (c['outbounds'] as List).cast<Map>().where((o) => '${o['tag']}'.startsWith('proxy-')).toList();

/// Writes configs for `xray run -test` when XRAY_TEST_OUT is set.
void dump(String name, Map<String, dynamic> config) {
  final out = Platform.environment['XRAY_TEST_OUT'];
  if (out == null) return;
  File('$out/$name.json')
    ..createSync(recursive: true)
    ..writeAsStringSync(const JsonEncoder.withIndent('  ').convert(config));
}

void main() {
  test('Fastest H2: a balancer over every endpoint, MASQUE over HTTP/2 with the account', () async {
    final b = await build('fastestH2', {
      'general': {'enableIpv6': true},
    });
    final out = proxies(b.config);
    expect(out.map((o) => o['settings']), [
      {'address': '162.159.198.2', 'port': 443},
      {'address': '2606:4700:103::2', 'port': 8443},
    ]);
    final stream = out.first['streamSettings'] as Map;
    expect(out.first['protocol'], 'masque');
    expect(stream['network'], 'masque');
    expect(stream['tlsSettings'], {
      'serverName': MasqueSettings.defaultSni,
      'alpn': ['h2'],
      'fingerprint': 'chrome',
    });
    expect(stream['masqueSettings'], {
      'warp': {
        'privateKey': account['privateKey'],
        'publicKey': 'MFkw', // the PEM fixture's body: base64, one line
        'address': [account['ipv4'], account['ipv6']],
      },
    });
    expect(stream['finalmask'], isNull);
    expect(b.config['routing']['balancers'][0]['tag'], 'all-proxies');
    expect(b.config['burstObservatory']['pingConfig']['interval'], '${const MasqueSettings().speedTestInterval}s');
    // WARP-like: the remote DNS is an IP through the tunnel, and UDP isn't blocked.
    final remote = (b.config['dns']['servers'] as List).whereType<Map>().firstWhere((s) => s['tag'] == 'remote-dns');
    expect(remote['address'], '1.1.1.1');
    expect(
      (b.config['routing']['rules'] as List).any((r) => r['network'] == 'udp' && r['outboundTag'] == 'block'),
      isFalse,
    );
    expect(b.remark, 'MASQUE H2 Best Ping 🚀');
    dump('masque_fastest_h2', b.config);
  });

  test('Fastest H3: the same over HTTP/3, without a TLS fingerprint', () async {
    final b = await build('fastestH3');
    final stream = proxies(b.config).single['streamSettings'] as Map; // IPv6 off: the IPv4 endpoint
    expect(stream['tlsSettings'], {
      'serverName': MasqueSettings.defaultSni,
      'alpn': ['h3'],
    });
    expect(b.remark, 'MASQUE H3 Best Ping 🚀');
    dump('masque_fastest_h3', b.config);
  });

  test("Smart Fragment H2: the first endpoint per fragment length, with MASQUE's own fragments", () async {
    final b = await build(
      'smartFragmentH2',
      masque({
        'fragments': [
          {
            'packets': '1-1',
            'lengths': [
              {'min': 1, 'max': 2},
            ],
            'delays': [
              {'min': 7, 'max': 9},
            ],
          },
        ],
      }),
    );
    final out = proxies(b.config);
    expect(out, hasLength(XrayConfigBuilder.smartLengths.length));
    for (var i = 0; i < out.length; i++) {
      expect(out[i]['settings'], {'address': '162.159.198.2', 'port': 443});
      final fragment = (out[i]['streamSettings']['finalmask']['tcp'] as List).single['settings'];
      expect(fragment['lengths'], [XrayConfigBuilder.smartLengths[i]]);
      expect(fragment['packets'], '1-1');
      expect(fragment['delays'], ['7-9']);
    }
    expect(b.remark, 'MASQUE H2 Smart Fragment 🧠');
    dump('masque_smart_fragment_h2', b.config);
  });

  test("Smart Obfuscation H3: the first endpoint per noise size, with MASQUE's own reset and delay", () async {
    final b = await build(
      'smartObfuscationH3',
      masque({
        'udpNoises': [
          {
            'type': 'rand',
            'size': {'min': 50, 'max': 100},
            'count': 1,
            'delay': {'min': 3, 'max': 4},
          },
        ],
        'udpNoiseReset': {'min': 10, 'max': 20},
      }),
    );
    final out = proxies(b.config);
    expect(out, hasLength(XrayConfigBuilder.smartLengths.length));
    for (var i = 0; i < out.length; i++) {
      final noise = (out[i]['streamSettings']['finalmask']['udp'] as List).single['settings'];
      expect(noise['reset'], '10-20');
      expect(noise['noise'], hasLength(5));
      expect(
        (noise['noise'] as List).every((n) => n['rand'] == XrayConfigBuilder.smartLengths[i] && n['delay'] == '3-4'),
        isTrue,
      );
      expect(out[i]['streamSettings']['tlsSettings']['alpn'], ['h3']);
    }
    expect(b.remark, 'MASQUE H3 Smart Obfuscation 🧠');
    dump('masque_smart_obfuscation_h3', b.config);
  });

  test('SNI, remote DNS and speed test interval from the MASQUE settings; no ECH', () async {
    final b = await build(
      'fastestH2',
      masque({
        'ech': {'enabled': true, 'dns': '8.8.8.8'}, // saved before ECH was dropped: ignored
        'sni': 'masque.example.com',
        'remoteDns': '9.9.9.9',
        'speedTestInterval': 30,
      }),
    );
    final tls = proxies(b.config).first['streamSettings']['tlsSettings'] as Map;
    expect(tls['serverName'], 'masque.example.com');
    expect(tls.containsKey('echConfigList'), isFalse);
    final remote = (b.config['dns']['servers'] as List).whereType<Map>().firstWhere((s) => s['tag'] == 'remote-dns');
    expect(remote['address'], '9.9.9.9');
    expect(b.config['burstObservatory']['pingConfig']['interval'], '30s');
  });

  test(
    'remote DNS by domain: its IPs in hosts, so reaching it needs no DNS; anti-sanction DoQ as quic+local',
    () async {
      final c = await XrayConfigBuilder(
        Profile.fromJson({
          ...masque({'remoteDns': 'https://dns.google/dns-query'}),
          'general': {'antiSanctionDns': 'quic://dns.adguard-dns.com'},
          'routing': {
            'sanctionBypass': {
              'presets': {'openAi': true},
            },
          },
        }),
        DeviceSettings.fromJson(const {
          'connection': {'masqueVariant': 'fastestH2'},
        }),
        resolver: (host, {required bool ipv6}) async => host == 'dns.google' ? const ['8.8.8.8', '8.8.4.4'] : const [],
      ).build(ConnectionMode.masque);
      final dns = c.config['dns'] as Map;
      expect(dns['hosts']['dns.google'], ['8.8.8.8', '8.8.4.4']);
      final servers = (dns['servers'] as List).whereType<Map>();
      expect(servers.firstWhere((s) => s['tag'] == 'remote-dns')['address'], 'https://dns.google/dns-query');
      expect(servers.map((s) => s['address']), contains('quic+local://dns.adguard-dns.com'));
    },
  );

  test('needs an account and an endpoint IPv4 can use', () async {
    final noAccount = XrayConfigBuilder(const Profile(), const DeviceSettings()).build(ConnectionMode.masque);
    expect(noAccount, throwsStateError);
    expect(
      () => build(
        'fastestH2',
        masque({
          'endpoints': ['[2606:4700:103::2]:443'],
        }),
      ),
      throwsStateError,
    );
  });

  test('a chain proxy exits through MASQUE', () async {
    final b = await build(
      'fastestH2',
      {
        'chains': [
          {'id': 'a1', 'name': 'socks', 'link': 'socks://user:pass@203.0.113.1:1080'},
        ],
      },
      {
        'exit': {'node': 'chain'},
      },
    );
    final exits = (b.config['outbounds'] as List).cast<Map>().where((o) => o['tag'] == 'exit');
    expect(exits.single['streamSettings']['sockopt']['dialerProxy'], 'exit-dialer');
    final rules = (b.config['routing']['rules'] as List).cast<Map>();
    final chainIn = rules.firstWhere((r) => (r['inboundTag'] as List?)?.contains('chain-in') ?? false);
    expect(chainIn['balancerTag'], 'all-proxies');
    expect((b.config['routing']['balancers'] as List).single['fallbackTag'], 'proxy-1');
    expect(b.remark, startsWith('🔗 '));
  });

  test("defaults: MASQUE's fragments and noises are the profile's, and its account isn't exported", () {
    const p = Profile();
    expect(p.masque.fragments.map((f) => f.toJson()), p.fragments.map((f) => f.toJson()));
    expect(p.masque.udpNoises.map((n) => n.toJson()), p.udpNoises.map((n) => n.toJson()));
    expect(p.masque.udpNoiseReset.toJson(), p.udpNoiseReset.toJson());
    expect(const DeviceSettings().masqueVariant, 'fastestH2');

    final withAccount = Profile.fromJson(masque());
    expect(withAccount.masque.account?.ipv4, '172.16.0.2');
    expect((withAccount.toExportJson()['masque'] as Map).containsKey('account'), isFalse);
    expect(const Profile().keepingPrivate(withAccount).masque.account, isNull);
    expect(withAccount.keepingPrivate(const Profile()).masque.account?.ipv6, account['ipv6']);
  });

  test("registration offers the MASQUE v4 / v6 addresses on each port, not WireGuard's host", () {
    expect(
      MasqueRegistrar.offeredEndpoints({
        'v4': '162.159.198.2:0',
        'v6': '[2606:4700:103::2]:0',
        'host': 'engage.cloudflareclient.com:2408',
        'ports': [443, 8443],
      }),
      ['162.159.198.2:443', '162.159.198.2:8443', '[2606:4700:103::2]:443', '[2606:4700:103::2]:8443'],
    );
  });

  test('P-256 keys: a SEC 1 private key and a PKIX public key', () {
    final keys = P256KeyPair.generate();
    expect(keys.point, hasLength(65));
    expect(keys.sec1.first, 0x30);
    expect(keys.spki, hasLength(91)); // the usual size of a P-256 SubjectPublicKeyInfo
    final out = Platform.environment['XRAY_TEST_OUT'];
    if (out != null) {
      File('$out/p256.key').writeAsStringSync(Der.pem('EC PRIVATE KEY', keys.sec1));
      File('$out/p256.pub').writeAsStringSync(Der.pem('PUBLIC KEY', keys.spki));
    }
  });
}
