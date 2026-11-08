import 'package:bpb/services/vpn_channel.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('start forwards the connection verification option', () async {
    const channel = MethodChannel('bpb/core');
    final calls = <MethodCall>[];
    final messenger =
        TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
    messenger.setMockMethodCallHandler(channel, (call) async {
      calls.add(call);
      return null;
    });
    addTearDown(() => messenger.setMockMethodCallHandler(channel, null));

    Future<void> start({bool verifyConnection = false}) => VpnChannel.start(
      config: const {},
      remark: 'Test',
      perAppMode: 'off',
      apps: const [],
      mtu: 1500,
      verifyConnection: verifyConnection,
    );

    await start(verifyConnection: true);
    await start();
    expect(calls.map((call) => call.method), ['start', 'start']);
    expect((calls.first.arguments as Map)['verifyConnection'], isTrue);
    expect((calls.last.arguments as Map)['verifyConnection'], isFalse);
  });
}
