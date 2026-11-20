import 'package:flutter/material.dart';

import '../models/settings.dart';
import '../state/app_state.dart';
import 'prefs.dart';
import 'theme.dart';

/// SSH endpoint and credentials used by the local SSH SOCKS proxy.
class SshSettingsPage extends StatefulWidget {
  const SshSettingsPage({super.key});

  @override
  State<SshSettingsPage> createState() => _SshSettingsPageState();
}

class _SshSettingsPageState extends State<SshSettingsPage> {
  final _formKey = GlobalKey<FormState>();
  late final SshSettings _initial;
  late final TextEditingController _host;
  late final TextEditingController _port;
  late final TextEditingController _user;
  late final TextEditingController _password;
  late final TextEditingController _key;
  late final TextEditingController _passphrase;
  late final TextEditingController _keepAlive;
  late final TextEditingController _tcpKeepAlive;
  bool _dirty = false;

  @override
  void initState() {
    super.initState();
    _initial = AppScope.read(context).profile.ssh;
    _host = TextEditingController(text: _initial.host)..addListener(_updateDirty);
    _port = TextEditingController(text: '${_initial.port}')..addListener(_updateDirty);
    _user = TextEditingController(text: _initial.user)..addListener(_updateDirty);
    _password = TextEditingController(text: _initial.password)..addListener(_updateDirty);
    _key = TextEditingController(text: _initial.privateKey)..addListener(_updateDirty);
    _passphrase = TextEditingController(text: _initial.passphrase)..addListener(_updateDirty);
    _keepAlive = TextEditingController(text: '${_initial.keepAliveInterval}')..addListener(_updateDirty);
    _tcpKeepAlive = TextEditingController(
      text: '${_initial.tcpKeepAliveInterval}',
    )..addListener(_updateDirty);
  }

  @override
  void dispose() {
    _host.dispose();
    _port.dispose();
    _user.dispose();
    _password.dispose();
    _key.dispose();
    _passphrase.dispose();
    _keepAlive.dispose();
    _tcpKeepAlive.dispose();
    super.dispose();
  }

  void _save() {
    if (!_formKey.currentState!.validate()) return;
    final state = AppScope.read(context);
    state.write('ssh.host', _host.text.trim());
    state.write('ssh.port', int.parse(_port.text));
    state.write('ssh.user', _user.text.trim());
    state.write('ssh.password', _password.text);
    state.write('ssh.privateKey', _key.text.trim());
    state.write('ssh.passphrase', _passphrase.text);
    state.write('ssh.keepAliveInterval', int.parse(_keepAlive.text));
    state.write(
      'ssh.tcpKeepAliveInterval',
      int.parse(_tcpKeepAlive.text),
    );
    if (mounted) setState(() => _dirty = false);
    Navigator.of(context).pop();
  }

  void _updateDirty() {
    final dirty = _host.text.trim() != _initial.host ||
        _port.text != '${_initial.port}' ||
        _user.text.trim() != _initial.user ||
        _password.text != _initial.password ||
        _key.text.trim() != _initial.privateKey ||
        _passphrase.text != _initial.passphrase ||
        _keepAlive.text != '${_initial.keepAliveInterval}' ||
        _tcpKeepAlive.text != '${_initial.tcpKeepAliveInterval}';
    if (_dirty != dirty && mounted) setState(() => _dirty = dirty);
  }

  Future<void> _confirmLeave() async {
    final choice = await showDialog<_LeaveChoice>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Save changes?'),
        content: const Text('You have unsaved SSH settings.'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(_LeaveChoice.cancel),
            child: const Text('Keep editing'),
          ),
          TextButton(
            onPressed: () => Navigator.of(context).pop(_LeaveChoice.discard),
            child: const Text('Discard'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(_LeaveChoice.save),
            child: const Text('Save'),
          ),
        ],
      ),
    );
    if (!mounted) return;
    switch (choice) {
      case _LeaveChoice.save:
        _save();
        break;
      case _LeaveChoice.discard:
        setState(() => _dirty = false);
        Navigator.of(context).pop();
        break;
      case _LeaveChoice.cancel:
      case null:
        break;
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    return PopScope(
      canPop: !_dirty,
      onPopInvokedWithResult: (didPop, _) async {
        if (!didPop && _dirty) await _confirmLeave();
      },
      child: Scaffold(
        appBar: AppBar(
          title: const Text('SSH settings'),
          actions: [TextButton(onPressed: _save, child: const Text('Save'))],
        ),
        body: Form(
          key: _formKey,
          child: ListView(
            padding: const EdgeInsets.only(bottom: 24),
            children: [
              const SectionHeader('Connection'),
              Card(
                margin: const EdgeInsets.symmetric(horizontal: 12),
                shape: roundedShape(
                  kHeroRadius,
                  side: BorderSide(
                    color: scheme.outlineVariant.withValues(alpha: 0.75),
                    width: 0.75,
                  ),
                ),
                elevation: 0,
                clipBehavior: Clip.antiAlias,
                child: Padding(
                  padding: const EdgeInsets.all(24),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    spacing: 16,
                    children: [
                      TextFormField(
                        controller: _host,
                        decoration: const InputDecoration(
                          labelText: 'IP or hostname',
                        ),
                        validator: _required,
                      ),
                      TextFormField(
                        controller: _port,
                        decoration: const InputDecoration(labelText: 'Port'),
                        keyboardType: TextInputType.number,
                        validator: (value) => _number(value, 1, 65535),
                      ),
                      TextFormField(
                        controller: _user,
                        decoration: const InputDecoration(
                          labelText: 'Username',
                        ),
                        validator: _required,
                      ),
                      TextFormField(
                        controller: _password,
                        decoration: const InputDecoration(
                          labelText: 'Password',
                          helperText:
                              'Use a password, a private key, or both.',
                        ),
                        obscureText: true,
                      ),
                      TextFormField(
                        controller: _key,
                        decoration: const InputDecoration(
                          labelText: 'Private key',
                          alignLabelWithHint: true,
                        ),
                        minLines: 4,
                        maxLines: 8,
                        autocorrect: false,
                      ),
                      TextFormField(
                        controller: _passphrase,
                        decoration: const InputDecoration(
                          labelText: 'Key passphrase',
                        ),
                        obscureText: true,
                      ),
                      TextFormField(
                        controller: _keepAlive,
                        decoration: const InputDecoration(
                          labelText: 'SSH keep alive interval (seconds)',
                          helperText: 'SSH session health check interval',
                        ),
                        keyboardType: TextInputType.number,
                        validator: (value) => _number(value, 5, 600),
                      ),
                      TextFormField(
                        controller: _tcpKeepAlive,
                        decoration: const InputDecoration(
                          labelText: 'TCP keep alive interval (seconds)',
                          helperText:
                              'Network TCP keep alive probes',
                        ),
                        keyboardType: TextInputType.number,
                        validator: (value) => _number(value, 1, 600),
                      ),
                    ],
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  String? _required(String? value) =>
      value == null || value.trim().isEmpty ? 'Required' : null;

  String? _number(String? value, int min, int max) {
    final number = int.tryParse(value ?? '');
    return number == null || number < min || number > max
        ? 'Enter a value from $min to $max'
        : null;
  }
}

enum _LeaveChoice { save, discard, cancel }
