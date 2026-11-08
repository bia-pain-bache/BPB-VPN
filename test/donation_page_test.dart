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
    expect(find.text('Tether · BSC'), findsOneWidget);
    expect(find.text('Tether · TRON'), findsOneWidget);
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
    for (final network in ['BSC', 'TRON', 'TON']) {
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
      ('BSC', 'test-bep20-address'),
      ('TRON', 'test-trc20-address'),
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
}
