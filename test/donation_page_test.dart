import 'package:bpb/state/app_state.dart';
import 'package:bpb/ui/donation_page.dart';
import 'package:bpb/ui/settings_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:qr_flutter/qr_flutter.dart';

void main() {
  testWidgets('Donation is the last About item and opens the page', (
    tester,
  ) async {
    await tester.pumpWidget(
      AppScope(
        state: AppState(),
        child: const MaterialApp(home: SettingsPage()),
      ),
    );
    await tester.scrollUntilVisible(
      find.text('Donation'),
      500,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.pumpAndSettle();
    expect(
      tester.getTopLeft(find.text('Donation')).dy,
      greaterThan(tester.getTopLeft(find.text('Open Source License')).dy),
    );
    await tester.tap(find.text('Donation'));
    await tester.pumpAndSettle();
    expect(find.byType(DonationPage), findsOneWidget);
    expect(find.text('Tether · BEP-20'), findsOneWidget);
    expect(find.text('Tether · TRC-20'), findsOneWidget);
    expect(find.text('Tether · TON'), findsOneWidget);
  });

  testWidgets('unconfigured addresses disable their actions', (tester) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: DonationPage(
          bep20Address: null,
          trc20Address: null,
          tonAddress: null,
        ),
      ),
    );
    expect(find.text('Address not configured'), findsNWidgets(3));
    for (final network in ['BEP-20', 'TRC-20', 'TON']) {
      for (final tooltip in ['Copy $network address', '$network QR code']) {
        expect(
          tester
              .widget<IconButton>(
                find.byWidgetPredicate(
                  (widget) => widget is IconButton && widget.tooltip == tooltip,
                ),
              )
              .onPressed,
          isNull,
        );
      }
    }
  });

  testWidgets('copy and QR actions use their respective configured addresses', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(320, 700);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    String? copied;
    final messenger = tester.binding.defaultBinaryMessenger;
    messenger.setMockMethodCallHandler(SystemChannels.platform, (call) async {
      if (call.method == 'Clipboard.setData') {
        copied = (call.arguments as Map)['text'] as String;
      }
      return null;
    });
    addTearDown(
      () => messenger.setMockMethodCallHandler(SystemChannels.platform, null),
    );
    await tester.pumpWidget(
      const MaterialApp(
        home: DonationPage(
          bep20Address: 'test-bep20-address',
          trc20Address: 'test-trc20-address',
          tonAddress: 'test-ton-address',
        ),
      ),
    );

    for (final (network, address) in [
      ('BEP-20', 'test-bep20-address'),
      ('TRC-20', 'test-trc20-address'),
      ('TON', 'test-ton-address'),
    ]) {
      await tester.tap(find.byTooltip('Copy $network address'));
      await tester.pump();
      expect(copied, address);
      await tester.tap(find.byTooltip('$network QR code'));
      await tester.pumpAndSettle();
      expect(find.byType(QrImageView), findsOneWidget);
      expect(
        find.descendant(
          of: find.byType(AlertDialog),
          matching: find.text(address),
        ),
        findsOneWidget,
      );
      await tester.tap(find.text('Close'));
      await tester.pumpAndSettle();
    }
    expect(tester.takeException(), isNull);
  });

  for (final screenSize in [
    const Size(320, 700),
    const Size(400, 700),
    const Size(800, 400),
  ]) {
    testWidgets('QR fills dialog width and stays square at $screenSize', (
      tester,
    ) async {
      tester.view.physicalSize = screenSize;
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);

      await tester.pumpWidget(const MaterialApp(home: DonationPage()));
      await tester.tap(find.byTooltip('BEP-20 QR code'));
      await tester.pumpAndSettle();

      final qr = find.byType(QrImageView);
      final content = find.ancestor(of: qr, matching: find.byType(Column)).first;
      final qrSize = tester.getSize(qr);
      expect(qrSize.width, tester.getSize(content).width);
      expect(qrSize.height, qrSize.width);
      expect(qrSize.width, screenSize.width - 80 - 48);
      expect(tester.takeException(), isNull);
    });
  }
}
