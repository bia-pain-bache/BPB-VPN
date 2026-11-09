import 'package:bpb/state/app_state.dart';
import 'package:bpb/ui/noises_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

class _TestState extends AppState {
  final writes = <String, Object?>{};

  @override
  Object? read(String path) {
    if (writes.containsKey(path)) return writes[path];
    if (path == 'udpNoises') {
      return [
        {
          'type': 'rand',
          'size': {'min': 50, 'max': 100},
          'count': 5,
          'delay': {'min': 1, 'max': 5},
        },
      ];
    }
    return super.read(path);
  }

  @override
  void write(String path, Object? value) => writes[path] = value;
}

void main() {
  testWidgets('expression packet presets select, edit, generate and restore', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(1200, 1800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final state = _TestState();
    addTearDown(state.dispose);
    Widget page() => AppScope(
      state: state,
      child: const MaterialApp(home: NoisesPage()),
    );
    await tester.pumpWidget(page());

    Finder field(String label) => find.ancestor(
      of: find.text(label),
      matching: find.byType(TextFormField),
    );
    Finder dropdown(String label) => find.ancestor(
      of: find.text(label),
      matching: find.byType(DropdownButtonFormField<String>),
    );
    Future<void> select(String label, String value) async {
      await tester.tap(dropdown(label));
      await tester.pumpAndSettle();
      await tester.tap(find.text(value).last);
      await tester.pumpAndSettle();
    }

    String packet() =>
        tester.widget<TextFormField>(field('Packet')).controller!.text;
    Future<void> save() async {
      await tester.pump();
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();
    }

    await select('Mode', 'Expression');
    expect(find.text('Packet preset'), findsOneWidget);
    expect(packet(), noiseExpPresets['QUIC Initial']);
    expect(find.byTooltip('Generate'), findsNothing);

    for (final preset in noiseExpPresets.entries) {
      await select('Packet preset', preset.key);
      expect(packet(), preset.value);
      expect(find.byTooltip('Generate'), findsNothing);
    }
    await save();
    await tester.pumpWidget(const SizedBox());
    await tester.pumpWidget(page());
    expect(packet(), noiseExpPresets['STUN Binding request']);
    expect(find.text('STUN Binding request'), findsOneWidget);

    await tester.enterText(field('Packet'), '<b 0x1234>');
    await tester.pumpAndSettle();
    expect(find.text('None'), findsOneWidget);
    expect(find.byTooltip('Generate'), findsOneWidget);
    await tester.tap(find.byTooltip('Generate'));
    await tester.pumpAndSettle();
    final generated = packet();
    expect(
      generated,
      matches(
        r'^<b 0x[0-9a-f]{4}01000001000000000000076578616d706c6503636f6d0000010001>$',
      ),
    );
    await save();
    expect(
      ((state.writes['udpNoises'] as List).single as Map)['packet'],
      generated,
    );
    await tester.pumpWidget(const SizedBox());
    await tester.pumpWidget(page());
    expect(find.text('None'), findsOneWidget);
    expect(packet(), generated);
    expect(find.byTooltip('Generate'), findsOneWidget);

    await select('Packet preset', 'DNS query');
    expect(packet(), noiseExpPresets['DNS query']);
    await select('Packet preset', 'None');
    expect(
      packet(),
      matches(
        r'^<b 0x[0-9a-f]{4}01000001000000000000076578616d706c6503636f6d0000010001>$',
      ),
    );
    expect(find.byTooltip('Generate'), findsOneWidget);
    await select('Mode', 'Random');
    expect(find.text('Packet preset'), findsNothing);
    expect(packet(), '50-100');
    expect(find.byTooltip('Generate'), findsOneWidget);
  });

  testWidgets('delay accepts zero and validates both bounds before saving', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(1200, 1400);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final state = _TestState();
    addTearDown(state.dispose);
    await tester.pumpWidget(
      AppScope(
        state: state,
        child: const MaterialApp(home: NoisesPage()),
      ),
    );

    Finder field(String label) => find.ancestor(
      of: find.text(label),
      matching: find.byType(TextFormField),
    );

    Future<void> save() async {
      await tester.pump();
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();
    }

    await tester.enterText(field('Delay min'), '0');
    await tester.enterText(field('Delay max'), '0');
    await save();
    expect(tester.takeException(), isNull);
    final saved = (state.writes['udpNoises'] as List).single as Map;
    expect(saved['delay'], {'min': 0, 'max': 0});

    state.writes.clear();
    await tester.enterText(field('Delay min'), '1');
    await save();
    expect(find.text('Min > max'), findsOneWidget);
    expect(state.writes, isEmpty);

    await tester.enterText(field('Delay min'), '');
    await tester.enterText(field('Delay max'), '');
    await save();
    expect(find.text('Enter 0 or more'), findsNWidgets(2));
    expect(state.writes, isEmpty);
    expect(tester.takeException(), isNull);

    await tester.enterText(field('Delay min'), '0');
    await tester.enterText(field('Delay max'), '5');
    await save();
    final savedRange = (state.writes['udpNoises'] as List).single as Map;
    expect(savedRange['delay'], {'min': 0, 'max': 5});
  });
}
