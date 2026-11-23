import 'package:bpb/models/settings.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  const withKey = Profile(
    ssh: SshSettings(
      host: 'h',
      user: 'u',
      password: 'p',
      hostKey: 'SHA256:abc',
    ),
  );

  test('the remembered SSH server key round-trips through the profile', () {
    expect(Profile.fromJson(withKey.toJson()).ssh.hostKey, 'SHA256:abc');
  });

  test('it is sent to the core with the other SSH settings', () {
    expect(withKey.ssh.toJson()['hostKey'], 'SHA256:abc');
  });

  test('forgetting it keeps the rest of the SSH settings', () {
    final cleared = withKey.updated('ssh.hostKey', null);
    expect(cleared.ssh.hostKey, '');
    expect(cleared.ssh.host, 'h');
    expect(cleared.ssh.password, 'p');
  });

  test('exported files carry no SSH settings, so no key either', () {
    expect(withKey.toExportJson().containsKey('ssh'), isFalse);
  });

  test('a stored profile from before the key existed still loads', () {
    final json = withKey.toJson();
    (json['ssh'] as Map).remove('hostKey');
    expect(Profile.fromJson(json).ssh.hostKey, '');
  });
}
