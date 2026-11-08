import 'package:bpb/services/scanner_channel.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('scanner events decode healthy IPs and delay', () {
    final event = ScannerEvent.fromJson({
      'type': 'done',
      'results': [
        {'ip': '192.0.2.1', 'httpMillis': 31.75},
      ],
    });

    expect(event.results, hasLength(1));
    expect(event.results.single.ip, '192.0.2.1');
    expect(event.results.single.httpMillis, 31.75);
  });
}
