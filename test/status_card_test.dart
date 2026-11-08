import 'package:bpb/state/app_state.dart';
import 'package:bpb/ui/status_card.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('connect button has equal top and right insets', (tester) async {
    await tester.pumpWidget(
      AppScope(
        state: AppState(),
        child: const MaterialApp(
          home: Scaffold(body: StatusCard()),
        ),
      ),
    );

    final card = tester.getRect(
      find.descendant(
        of: find.byType(Card),
        matching: find.byType(Stack),
      ).first,
    );
    final ink = tester.getRect(find.byType(InkWell));
    expect(ink.top - card.top, 24);
    expect(card.right - ink.right, 24);
  });
}
