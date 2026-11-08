import 'dart:convert';

import 'package:bpb/models/settings.dart';
import 'package:bpb/services/scanner_channel.dart';
import 'package:bpb/services/warp.dart';
import 'package:bpb/state/app_state.dart';
import 'package:bpb/ui/cloudflare_scanner_page.dart';
import 'package:bpb/ui/prefs.dart';
import 'package:flutter/material.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

class _ScannerTestState extends AppState {
  @override
  Profile get profile => const Profile(
    warp: WarpSettings(
      account: WarpAccount(
        privateKey: 'private',
        publicKey: 'public',
        ipv6: '2606:4700::1/128',
        reserved: [1, 2, 3],
      ),
    ),
  );
}

void main() {
  testWidgets('scan disables and re-enables all settings', (tester) async {
    debugDefaultTargetPlatformOverride = TargetPlatform.android;
    addTearDown(() => debugDefaultTargetPlatformOverride = null);
    tester.view.physicalSize = const Size(1000, 1200);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    const methods = MethodChannel('bpb/core');
    const events = MethodChannel('bpb/scanner');
    final messenger = tester.binding.defaultBinaryMessenger;
    messenger.setMockMethodCallHandler(methods, (call) async => null);
    messenger.setMockMethodCallHandler(events, (call) async => null);
    addTearDown(() {
      messenger.setMockMethodCallHandler(methods, null);
      messenger.setMockMethodCallHandler(events, null);
    });
    await tester.runAsync(
      () => rootBundle.loadString('assets/scanner/warp-cidrs.txt'),
    );
    await tester.pumpWidget(
      AppScope(
        state: _ScannerTestState(),
        child: const MaterialApp(
          home: CloudflareScannerPage(protocol: ScanProtocol.warp),
        ),
      ),
    );
    await tester.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 100)),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Custom CIDRs'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Select CIDRs'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('162.159.192.0/24'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Apply'));
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.text('Start scan'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Start scan'));
    await tester.pump();

    void expectSettingsEnabled(bool enabled) {
      final switchTile = tester.widget<SwitchListTile>(
        find.byType(SwitchListTile),
      );
      expect(switchTile.onChanged != null, enabled);
      final cidrTile = tester.widget<ListTile>(
        find.ancestor(
          of: find.text('Select CIDRs'),
          matching: find.byType(ListTile),
        ),
      );
      expect(cidrTile.enabled, enabled);
      expect(cidrTile.onTap != null, enabled);
      for (final field in tester.widgetList<TextField>(
        find.byType(TextField),
      )) {
        expect(field.enabled, enabled);
      }
      final decorators = tester.widgetList<InputDecorator>(
        find.byType(InputDecorator),
      );
      expect(decorators, hasLength(7));
      for (final decorator in decorators) {
        expect(decorator.decoration.enabled, enabled);
      }
    }

    expectSettingsEnabled(false);
    await tester.tap(find.text('Select CIDRs'));
    await tester.pump();
    expect(find.byType(AlertDialog), findsNothing);
    await messenger.handlePlatformMessage(
      'bpb/scanner',
      const StandardMethodCodec().encodeSuccessEnvelope(
        jsonEncode({'type': 'done'}),
      ),
      (_) {},
    );
    await tester.pumpAndSettle();
    expectSettingsEnabled(true);
    expect(find.text('No candidates passed all attempts.'), findsOneWidget);
    expect(find.textContaining('Real delay test:'), findsNothing);
    expect(find.textContaining('Last probe error:'), findsNothing);
    await tester.tap(find.text('Select CIDRs'));
    await tester.pumpAndSettle();
    expect(find.byType(AlertDialog), findsOneWidget);
    expect(tester.widget<Checkbox>(find.byType(Checkbox).first).value, true);
    debugDefaultTargetPlatformOverride = null;
  });

  testWidgets('scan displays IP and delay from simplified result', (
    tester,
  ) async {
    debugDefaultTargetPlatformOverride = TargetPlatform.android;
    addTearDown(() => debugDefaultTargetPlatformOverride = null);
    tester.view.physicalSize = const Size(1000, 1200);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final messenger = tester.binding.defaultBinaryMessenger;
    const methods = MethodChannel('bpb/core');
    const events = MethodChannel('bpb/scanner');
    Map<String, dynamic>? sentOptions;
    messenger.setMockMethodCallHandler(methods, (call) async {
      if (call.method == 'scannerStart') {
        final arguments = Map<String, dynamic>.from(call.arguments as Map);
        sentOptions =
            jsonDecode(arguments['config'] as String) as Map<String, dynamic>;
      }
      return null;
    });
    messenger.setMockMethodCallHandler(events, (call) async => null);
    addTearDown(() {
      messenger.setMockMethodCallHandler(methods, null);
      messenger.setMockMethodCallHandler(events, null);
    });
    await tester.runAsync(
      () => rootBundle.loadString('assets/scanner/warp-cidrs.txt'),
    );
    await tester.pumpWidget(
      AppScope(
        state: _ScannerTestState(),
        child: const MaterialApp(
          home: CloudflareScannerPage(protocol: ScanProtocol.warp),
        ),
      ),
    );
    await tester.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 100)),
    );
    await tester.pumpAndSettle();
    Finder input(String label) => find.byWidgetPredicate(
      (widget) => widget is TextField && widget.decoration?.labelText == label,
    );
    await tester.enterText(input('Probes count'), '42');
    await tester.enterText(input('Probes interval (ms)'), '180');
    await tester.enterText(input('Attempts interval (ms)'), '75');
    await tester.tap(find.byType(DropdownButtonFormField<int>).last);
    await tester.pumpAndSettle();
    await tester.tap(find.text('4').last);
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.text('Start scan'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Start scan'));
    await tester.pump();
    expect(sentOptions, containsPair('probeCount', 42));
    expect(sentOptions, containsPair('probeInterval', 180));
    expect(sentOptions, containsPair('attemptCount', 4));
    expect(sentOptions, containsPair('attemptInterval', 75));
    expect(sentOptions!.keys, isNot(contains('trials')));
    expect(sentOptions!.keys, isNot(contains('trialCount')));
    expect(sentOptions!.keys, isNot(contains('trialInterval')));
    expect(sentOptions!.keys, isNot(contains('ipIntervalMs')));
    expect(sentOptions!.keys, isNot(contains('trialIntervalMs')));
    await messenger.handlePlatformMessage(
      'bpb/scanner',
      const StandardMethodCodec().encodeSuccessEnvelope(
        jsonEncode({
          'type': 'done',
          'results': [
            {'ip': '192.0.2.1', 'httpMillis': 31.75},
          ],
        }),
      ),
      (_) {},
    );
    await tester.pumpAndSettle();
    await tester.scrollUntilVisible(
      find.text('192.0.2.1'),
      200,
      scrollable: find
          .descendant(
            of: find.byType(Scaffold),
            matching: find.byType(Scrollable),
          )
          .first,
    );
    expect(find.text('192.0.2.1'), findsOneWidget);
    expect(find.text('Real delay 31.75 ms'), findsOneWidget);
    debugDefaultTargetPlatformOverride = null;
  });

  for (final (protocol, alternate, label) in [
    (ScanProtocol.vless, 'fragment', 'Fragment'),
    (ScanProtocol.warp, 'noise', 'Obfuscated'),
    (ScanProtocol.masqueH2, 'fragment', 'Fragment'),
    (ScanProtocol.masqueH3, 'noise', 'Obfuscated'),
  ]) {
    testWidgets('${protocol.label} offers the right scanner modes', (
      tester,
    ) async {
      tester.view.physicalSize = const Size(1000, 1200);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      await tester.runAsync(
        () => rootBundle.loadString(
          protocol.isWarpLike
              ? 'assets/scanner/warp-cidrs.txt'
              : 'assets/scanner/vless-cidrs.txt',
        ),
      );
      await tester.pumpWidget(
        AppScope(
          state: AppState(),
          child: MaterialApp(home: CloudflareScannerPage(protocol: protocol)),
        ),
      );
      await tester.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 100)),
      );
      await tester.pumpAndSettle();
      expect(find.text('Protocol'), findsNothing);
      expect(find.text('Attempts interval (ms)'), findsOneWidget);
      expect(find.text('Probes interval (ms)'), findsOneWidget);
      expect(find.text('Batch size'), findsNothing);
      expect(find.text('50'), findsOneWidget);
      expect(find.text('100'), findsNWidgets(2));
      final menu = tester.widget<DropdownButtonFormField<String>>(
        find.byType(DropdownButtonFormField<String>),
      );
      expect(menu.initialValue, 'normal');
      final dropdown = tester.widget<DropdownButton<String>>(
        find.byType(DropdownButton<String>),
      );
      expect(dropdown.items!.map((item) => item.value), ['normal', alternate]);
      expect(find.byType(SettingsCard), findsNothing);
      await tester.tap(find.text('Normal'));
      await tester.pumpAndSettle();
      await tester.tap(find.text(label).last);
      await tester.pumpAndSettle();
      expect(
        tester
            .widget<DropdownButtonFormField<String>>(
              find.byType(DropdownButtonFormField<String>),
            )
            .initialValue,
        alternate,
      );
    });
  }

  testWidgets('scan settings card has the requested layout', (tester) async {
    tester.view.physicalSize = const Size(1000, 1200);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.runAsync(
      () => rootBundle.loadString('assets/scanner/warp-cidrs.txt'),
    );
    await tester.pumpWidget(
      AppScope(
        state: AppState(),
        child: const MaterialApp(
          home: CloudflareScannerPage(protocol: ScanProtocol.warp),
        ),
      ),
    );
    await tester.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 100)),
    );
    await tester.pumpAndSettle();

    expect(find.text('Connection'), findsNothing);
    expect(find.text('Scan settings'), findsOneWidget);
    final card = find.byType(Card);
    expect(card, findsOneWidget);
    expect(
      tester.widget<Card>(card).margin,
      const EdgeInsets.symmetric(horizontal: 12),
    );
    final column = find
        .descendant(of: card, matching: find.byType(Column))
        .first;
    expect(tester.widget<Column>(column).spacing, 16);
    final padding = tester.widget<Padding>(
      find.ancestor(of: column, matching: find.byType(Padding)).first,
    );
    expect(padding.padding, const EdgeInsets.all(24));
    expect(
      find.descendant(of: card, matching: find.byType(Divider)),
      findsNothing,
    );
    final fields = [
      find.byType(DropdownButtonFormField<String>),
      find.byType(DropdownButtonFormField<int>).first,
      ...find
          .byType(TextField)
          .evaluate()
          .map((element) => find.byWidget(element.widget)),
      find.byType(DropdownButtonFormField<int>).last,
    ];
    final orderedFields = [
      fields[0],
      fields[1],
      fields[2],
      fields[3],
      fields[6],
      fields[4],
      fields[5],
    ];
    for (var i = 1; i < orderedFields.length; i++) {
      expect(
        tester.getTopLeft(orderedFields[i]).dy -
            tester.getBottomLeft(orderedFields[i - 1]).dy,
        16,
      );
    }

    expect(
      tester.getTopLeft(find.text('Custom CIDRs')).dy,
      greaterThan(tester.getBottomLeft(orderedFields.last).dy),
    );
  });

  testWidgets('custom CIDRs persist on the page and selected entries lead', (
    tester,
  ) async {
    await tester.runAsync(
      () => rootBundle.loadString('assets/scanner/warp-cidrs.txt'),
    );
    await tester.pumpWidget(
      AppScope(
        state: AppState(),
        child: const MaterialApp(
          home: CloudflareScannerPage(protocol: ScanProtocol.warp),
        ),
      ),
    );
    await tester.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 100)),
    );
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.text('Custom CIDRs'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Custom CIDRs'));
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.text('0 of 21 CIDRs selected'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('0 of 21 CIDRs selected'));
    await tester.pumpAndSettle();
    final firstTile = tester.widget<ListTile>(find.byType(ListTile).last);
    expect(tester.widget<Checkbox>(find.byType(Checkbox).first).value, false);
    expect(firstTile.contentPadding, EdgeInsets.zero);
    expect(
      tester.getTopLeft(find.byType(Checkbox).first).dx,
      tester.getTopLeft(find.byType(TextField).last).dx - 11,
    );
    await tester.enterText(find.byType(TextField).last, '162.159.198');
    await tester.pumpAndSettle();
    expect(find.text('162.159.198.0/24'), findsOneWidget);
    await tester.tap(find.text('162.159.198.0/24'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField).last, '');
    await tester.pumpAndSettle();
    expect(
      tester.getTopLeft(find.text('162.159.198.0/24')).dy,
      lessThan(tester.getTopLeft(find.text('162.159.192.0/24')).dy),
    );
    await tester.tap(find.text('162.159.195.0/24'));
    await tester.pumpAndSettle();
    expect(
      tester.getTopLeft(find.text('162.159.195.0/24')).dy,
      lessThan(tester.getTopLeft(find.text('162.159.198.0/24')).dy),
    );
    expect(
      tester.getTopLeft(find.text('162.159.198.0/24')).dy,
      lessThan(tester.getTopLeft(find.text('162.159.192.0/24')).dy),
    );
    await tester.tap(find.text('Apply'));
    await tester.pumpAndSettle();
    expect(find.text('2 of 21 CIDRs selected'), findsOneWidget);
    await tester.tap(find.text('Custom CIDRs'));
    await tester.pumpAndSettle();
    expect(find.text('2 of 21 CIDRs selected'), findsNothing);
    await tester.tap(find.text('Custom CIDRs'));
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.text('2 of 21 CIDRs selected'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('2 of 21 CIDRs selected'));
    await tester.pumpAndSettle();
    expect(tester.widget<Checkbox>(find.byType(Checkbox).first).value, true);
    expect(
      tester.getTopLeft(find.text('162.159.195.0/24')).dy,
      lessThan(tester.getTopLeft(find.text('162.159.192.0/24')).dy),
    );
    await tester.tap(find.text('162.159.195.0/24'));
    await tester.pumpAndSettle();
    expect(
      tester.getTopLeft(find.text('162.159.198.0/24')).dy,
      lessThan(tester.getTopLeft(find.text('162.159.195.0/24')).dy),
    );
    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();
    expect(find.text('2 of 21 CIDRs selected'), findsOneWidget);
    await tester.tap(find.text('2 of 21 CIDRs selected'));
    await tester.pumpAndSettle();
    expect(tester.widget<Checkbox>(find.byType(Checkbox).first).value, true);
    await tester.tap(find.text('162.159.195.0/24'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Apply'));
    await tester.pumpAndSettle();
    expect(find.text('1 of 21 CIDRs selected'), findsOneWidget);
  });

  testWidgets('selected CIDR remains visible after selecting from lower down', (
    tester,
  ) async {
    await tester.runAsync(
      () => rootBundle.loadString('assets/scanner/warp-cidrs.txt'),
    );
    await tester.pumpWidget(
      AppScope(
        state: AppState(),
        child: const MaterialApp(
          home: CloudflareScannerPage(protocol: ScanProtocol.warp),
        ),
      ),
    );
    await tester.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 100)),
    );
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.text('Custom CIDRs'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Custom CIDRs'));
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.text('0 of 21 CIDRs selected'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('0 of 21 CIDRs selected'));
    await tester.pumpAndSettle();
    final list = find.descendant(
      of: find.byType(AlertDialog),
      matching: find.byType(ListView),
    );
    await tester.scrollUntilVisible(
      find.text('8.39.204.0/24'),
      200,
      scrollable: find.descendant(of: list, matching: find.byType(Scrollable)),
    );
    await tester.ensureVisible(find.text('8.39.204.0/24'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('8.39.204.0/24'));
    await tester.pumpAndSettle();
    expect(find.text('8.39.204.0/24').hitTestable(), findsOneWidget);
    await tester.tap(find.text('Apply'));
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.text('1 of 21 CIDRs selected'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('1 of 21 CIDRs selected'));
    await tester.pumpAndSettle();
    expect(find.text('8.39.204.0/24').hitTestable(), findsOneWidget);
    expect(tester.widget<Checkbox>(find.byType(Checkbox).first).value, true);
  });
}
