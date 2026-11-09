import 'dart:convert';

import 'package:bpb/models/cf_account.dart';
import 'package:bpb/models/settings.dart';
import 'package:bpb/state/app_state.dart';
import 'package:bpb/ui/prefs.dart';
import 'package:bpb/ui/settings_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _EditingState extends AppState {
  final writes = <String, Object?>{};

  @override
  List<String> readList(String path) => [];

  @override
  void write(String path, Object? value) => writes[path] = value;
}

void main() {
  testWidgets('reset preserves endpoints and selected deployment clean IPs', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({
      'profile': jsonEncode({
        'general': {'localDns': '9.9.9.9'},
        'warp': {
          'endpoints': ['162.159.192.1:2408', '162.159.192.2:2408'],
          'keepAlive': 20,
        },
        'masque': {
          'endpoints': ['162.159.198.1:443', '162.159.198.2:443'],
          'transport': 'h3',
        },
        'tor': {
          'bridges': ['obfs4 192.0.2.1:443 fingerprint cert=x iat-mode=0'],
        },
      }),
    });
    FlutterSecureStorage.setMockInitialValues({});
    final messenger = tester.binding.defaultBinaryMessenger;
    messenger.setMockMethodCallHandler(
      const MethodChannel('bpb/state'),
      (_) async => null,
    );
    addTearDown(
      () => messenger.setMockMethodCallHandler(
        const MethodChannel('bpb/state'),
        null,
      ),
    );

    final state = AppState();
    await state.init();
    state.cfAccounts = [
      const SavedCfAccount(
        id: 'a',
        name: 'a',
        token: 'token',
        uuid: 'uuid',
        deployments: [
          Deployment(
            type: WorkerType.workers,
            name: 'worker',
            host: 'worker.example.com',
            version: 1,
            vless: VlessSettings(cleanIps: ['104.16.1.1'], ports: [8443]),
          ),
          Deployment(
            type: WorkerType.pages,
            name: 'pages',
            host: 'pages.example.com',
            version: 1,
            vless: VlessSettings(cleanIps: ['104.16.2.2'], ports: [2053]),
          ),
        ],
      ),
    ];
    final bridges = state.profile.tor.bridges;
    state.resetSettings();
    await tester.pump();

    expect(state.profile.vless.cleanIps, ['104.16.1.1']);
    expect(state.profile.vless.ports, const VlessSettings().ports);
    expect(state.cfAccounts.single.deployment(WorkerType.pages)!.vless.ports, [
      2053,
    ]);
    expect(
      state.cfAccounts.single.deployment(WorkerType.pages)!.vless.cleanIps,
      ['104.16.2.2'],
    );
    expect(state.profile.warp.endpoints, [
      '162.159.192.1:2408',
      '162.159.192.2:2408',
    ]);
    expect(state.profile.masque.endpoints, [
      '162.159.198.1:443',
      '162.159.198.2:443',
    ]);
    expect(state.profile.general.localDns, const GeneralSettings().localDns);
    expect(state.profile.warp.keepAlive, const WarpSettings().keepAlive);
    expect(state.profile.masque.transport, const MasqueSettings().transport);
    expect(state.profile.tor.bridges, bridges);

    final prefs = await SharedPreferences.getInstance();
    final saved = Profile.fromJson(
      jsonDecode(prefs.getString('profile')!) as Map<String, dynamic>,
    );
    expect(saved.warp.endpoints, state.profile.warp.endpoints);
    expect(saved.masque.endpoints, state.profile.masque.endpoints);
    final accounts = jsonDecode(
      (await const FlutterSecureStorage().read(key: 'cloudflareAccounts'))!,
    ) as List;
    final savedAccount = SavedCfAccount.fromJson(
      Map<String, dynamic>.from(accounts.single),
    )!;
    expect(savedAccount.deployment(WorkerType.workers)!.vless.cleanIps, [
      '104.16.1.1',
    ]);
    state.dispose();
  });

  for (final scenario in [
    (
      path: 'vless.cleanIps',
      invalid: 'example.com\n2606:4700::1',
      message: 'Add at least one IPv4 address',
      valid: 'example.com\n104.16.1.1',
      expected: ['example.com', '104.16.1.1'],
    ),
    (
      path: 'warp.endpoints',
      invalid: '162.159.192.1:2408\n[2606:4700::1]:2408',
      message: 'Add at least two IPv4 endpoints',
      valid: '162.159.192.1:2408\n162.159.192.2:2408',
      expected: ['162.159.192.1:2408', '162.159.192.2:2408'],
    ),
    (
      path: 'masque.endpoints',
      invalid: '162.159.198.1:443\nexample.com:443',
      message: 'Add at least two IPv4 endpoints',
      valid: '162.159.198.1:443\n162.159.198.2:443',
      expected: ['162.159.198.1:443', '162.159.198.2:443'],
    ),
  ]) {
    testWidgets('${scenario.path} validates before saving', (tester) async {
      final state = _EditingState();
      addTearDown(state.dispose);
      await tester.pumpWidget(
        AppScope(
          state: state,
          child: const MaterialApp(home: SettingsPage()),
        ),
      );
      final pref = find.byWidgetPredicate(
        (w) => w is ListPref && w.path == scenario.path,
      );
      await tester.scrollUntilVisible(
        pref,
        300,
        scrollable: find.byType(Scrollable).first,
      );
      await tester.tap(pref);
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField), '');
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();
      expect(find.text(scenario.message), findsOneWidget);
      await tester.enterText(find.byType(TextField), scenario.invalid);
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();
      expect(find.text(scenario.message), findsOneWidget);
      expect(state.writes, isEmpty);
      await tester.enterText(find.byType(TextField), scenario.valid);
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();
      expect(state.writes[scenario.path], scenario.expected);
    });
  }
}
