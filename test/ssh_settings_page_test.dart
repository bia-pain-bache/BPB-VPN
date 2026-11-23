import 'package:bpb/state/app_state.dart';
import 'package:bpb/ui/ssh_settings_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

class _TestState extends AppState {
  final writes = <String, Object?>{};

  @override
  void write(String path, Object? value) => writes[path] = value;
}

void main() {
  late _TestState state;

  Future<void> openPage(WidgetTester tester) async {
    tester.view.physicalSize = const Size(1200, 2400);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    state = _TestState();
    addTearDown(state.dispose);
    await tester.pumpWidget(
      AppScope(
        state: state,
        child: MaterialApp(
          home: Builder(
            builder: (context) => Scaffold(
              body: TextButton(
                onPressed: () => Navigator.of(context).push(
                  MaterialPageRoute<void>(
                    builder: (_) => const SshSettingsPage(),
                  ),
                ),
                child: const Text('open'),
              ),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();
  }

  Finder field(String label) =>
      find.ancestor(of: find.text(label), matching: find.byType(TextFormField));

  bool hidden(WidgetTester tester, String label) => tester
      .widget<TextField>(
        find.descendant(of: field(label), matching: find.byType(TextField)),
      )
      .obscureText;

  Finder eye(String label) =>
      find.descendant(of: field(label), matching: find.byType(IconButton));

  Future<void> fill(WidgetTester tester, Map<String, String> values) async {
    for (final entry in values.entries) {
      await tester.enterText(field(entry.key), entry.value);
    }
    await tester.pump();
  }

  Future<void> save(WidgetTester tester) async {
    await tester.tap(find.text('Save').first);
    await tester.pumpAndSettle();
  }

  testWidgets(
    'the password and the passphrase are hidden, each with its own eye',
    (tester) async {
      await openPage(tester);
      expect(hidden(tester, 'Password'), isTrue);
      expect(hidden(tester, 'Key passphrase'), isTrue);

      await tester.tap(eye('Password'));
      await tester.pump();
      expect(hidden(tester, 'Password'), isFalse);
      expect(hidden(tester, 'Key passphrase'), isTrue);

      await tester.tap(eye('Key passphrase'));
      await tester.pump();
      expect(hidden(tester, 'Key passphrase'), isFalse);

      await tester.tap(eye('Password'));
      await tester.pump();
      expect(hidden(tester, 'Password'), isTrue);
    },
  );

  testWidgets('a password or a private key is needed', (tester) async {
    await openPage(tester);
    await fill(tester, {'IP or hostname': 'h', 'Username': 'u'});
    await save(tester);
    expect(find.text('Enter a password or a private key'), findsOneWidget);
    expect(state.writes, isEmpty);

    await fill(tester, {'Password': 'p'});
    await save(tester);
    expect(state.writes['ssh.host'], 'h');
    expect(state.writes['ssh.password'], 'p');
  });

  testWidgets('a key pasted wrongly is explained before connecting', (
    tester,
  ) async {
    await openPage(tester);
    await fill(tester, {'IP or hostname': 'h', 'Username': 'u'});

    for (final (key, message) in [
      (
        'ssh-ed25519 AAAAC3Nza user@host',
        'That is a public key. Paste the private key.',
      ),
      (
        'PuTTY-User-Key-File-3: ssh-ed25519',
        "PuTTY (.ppk) keys aren't supported. Export it as an OpenSSH key.",
      ),
      ('hello', 'Paste the whole key, with its BEGIN and END lines.'),
    ]) {
      await fill(tester, {'Private key': key});
      await save(tester);
      expect(find.text(message), findsOneWidget, reason: key);
      expect(state.writes, isEmpty);
    }

    await fill(tester, {
      'Private key': '-----BEGIN OPENSSH PRIVATE KEY-----\nAAAA\n-----END OPENSSH PRIVATE KEY-----',
    });
    await save(tester);
    expect(state.writes['ssh.privateKey'], contains('BEGIN OPENSSH'));
  });

  testWidgets('a new host or port forgets the remembered server key', (
    tester,
  ) async {
    await openPage(tester);
    await fill(tester, {
      'IP or hostname': 'h',
      'Username': 'u',
      'Password': 'p',
    });
    await save(tester);
    expect(state.writes.containsKey('ssh.hostKey'), isTrue);
    expect(state.writes['ssh.hostKey'], isNull);
  });
}
