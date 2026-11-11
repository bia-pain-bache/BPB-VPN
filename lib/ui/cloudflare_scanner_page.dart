import 'dart:async';

import 'package:flutter/material.dart';
import 'package:material_symbols_icons/symbols.dart';

import '../services/scanner_channel.dart';
import '../services/xray_config.dart';
import '../state/app_state.dart';
import 'prefs.dart';
import 'theme.dart';

class CloudflareScannerPage extends StatefulWidget {
  const CloudflareScannerPage({super.key, required this.protocol});

  final ScanProtocol protocol;

  @override
  State<CloudflareScannerPage> createState() => _CloudflareScannerPageState();
}

class _CloudflareScannerPageState extends State<CloudflareScannerPage> {
  final _probeCount = TextEditingController(text: '100');
  final _outputCount = TextEditingController(text: '10');
  final _attemptInterval = TextEditingController(text: '50');
  final _probeInterval = TextEditingController(text: '100');
  ScanProtocol get _protocol => widget.protocol;
  String _mode = 'normal';
  bool _customCidrs = false;
  Set<String> _selectedCidrs = {};
  List<String> _cidrs = const [];
  StreamSubscription<ScannerEvent>? _events;
  bool _loadingCidrs = true;
  bool _running = false;
  bool _paused = false;
  int _attemptCount = 3;
  late int _port;
  int? _resultPort;
  int _completed = 0;
  int _total = 0;
  String? _phase;
  String? _error;
  List<ScanResult> _results = const [];
  Set<String> _selectedResults = {};

  @override
  void initState() {
    super.initState();
    _port = _protocol == ScanProtocol.warp ? 2408 : 443;
    unawaited(_loadCidrs());
  }

  @override
  void dispose() {
    _events?.cancel();
    _probeCount.dispose();
    _outputCount.dispose();
    _attemptInterval.dispose();
    _probeInterval.dispose();
    super.dispose();
  }

  Future<void> _loadCidrs() async {
    setState(() {
      _loadingCidrs = true;
      _error = null;
      _cidrs = const [];
      _selectedCidrs = {};
    });
    try {
      final values = await ScannerAssets.loadCidrs(
        warpLike: _protocol.isWarpLike,
      );
      if (!mounted) return;
      setState(() {
        _cidrs = values;
        _selectedCidrs = {};
        _loadingCidrs = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _error = error.toString();
        _loadingCidrs = false;
      });
    }
  }

  Future<void> _selectCidrs() async {
    final selected = Set<String>.of(_selectedCidrs);
    var query = '';
    final result = await showDialog<Set<String>>(
      context: context,
      builder: (context) => StatefulBuilder(
        builder: (context, update) => AlertDialog(
          title: const Text('Select CIDRs'),
          content: SizedBox(
            width: 420,
            height: 420,
            child: Column(
              spacing: 12,
              children: [
                TextField(
                  decoration: const InputDecoration(labelText: 'Search CIDRs'),
                  onChanged: (value) =>
                      update(() => query = value.trim().toLowerCase()),
                ),
                Expanded(
                  child: Builder(
                    builder: (context) {
                      final filtered = _cidrs
                          .where((cidr) => cidr.toLowerCase().contains(query))
                          .toList();
                      final ordered = [
                        ...filtered.where(selected.contains),
                        ...filtered.where((cidr) => !selected.contains(cidr)),
                      ];
                      return ListView.builder(
                        primary: false,
                        itemCount: ordered.length,
                        itemBuilder: (context, index) {
                          final cidr = ordered[index];
                          final isSelected = selected.contains(cidr);
                          void toggle() {
                            update(
                              () => isSelected
                                  ? selected.remove(cidr)
                                  : selected.add(cidr),
                            );
                            if (!isSelected) {
                              Scrollable.of(context).position.jumpTo(0);
                            }
                          }

                          return MergeSemantics(
                            child: ListTile(
                              dense: true,
                              contentPadding: EdgeInsets.zero,
                              leading: Transform.translate(
                                offset: const Offset(-11, 0),
                                child: Checkbox(
                                  value: isSelected,
                                  materialTapTargetSize:
                                      MaterialTapTargetSize.shrinkWrap,
                                  onChanged: (_) => toggle(),
                                ),
                              ),
                              title: Text(cidr),
                              onTap: toggle,
                            ),
                          );
                        },
                      );
                    },
                  ),
                ),
              ],
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context),
              child: const Text('Cancel'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(context, selected),
              child: const Text('Apply'),
            ),
          ],
        ),
      ),
    );
    if (result != null) setState(() => _selectedCidrs = result);
  }

  Future<void> _start(AppState state) async {
    final probeCount = int.tryParse(_probeCount.text);
    final output = int.tryParse(_outputCount.text);
    final attemptInterval = int.tryParse(_attemptInterval.text);
    final probeInterval = int.tryParse(_probeInterval.text);

    if (probeCount == null || probeCount < 10 || probeCount > 1000000) {
      setState(() => _error = 'Probe count must be between 10 and 1000000.');
      return;
    }
    if (output == null || output < 1 || output > probeCount) {
      setState(() => _error = 'Output count must be 1 and $probeCount.');
      return;
    }
    if (attemptInterval == null ||
        attemptInterval < 10 ||
        attemptInterval > 100) {
      setState(
        () => _error = 'Attempt interval must be between 10 and 100 ms.',
      );
      return;
    }
    if (probeInterval == null || probeInterval < 100 || probeInterval > 1000) {
      setState(
        () => _error = 'Probe interval must be between 100 and 1000 ms.',
      );
      return;
    }
    final cidrs = _customCidrs ? _selectedCidrs.toList() : _cidrs;
    if (cidrs.isEmpty) {
      setState(() => _error = 'Select at least one CIDR.');
      return;
    }
    final worker = state.worker;
    if (_protocol == ScanProtocol.vless && worker == null) {
      setState(
        () => _error = 'Set up a VLESS deployment before scanning VLESS IPs.',
      );
      return;
    }

    final masque = state.profile.masque;
    final modeData = _protocol == ScanProtocol.warp
        ? {
            'noises': state.profile.udpNoises
                .map((item) => item.toJson())
                .toList(),
            'noiseReset': state.profile.udpNoiseReset.toJson(),
          }
        : _protocol.isMasque
        ? _protocol == ScanProtocol.masqueH3
              ? {
                  'noises': masque.udpNoises
                      .map((item) => item.toJson())
                      .toList(),
                  'noiseReset': masque.udpNoiseReset.toJson(),
                }
              : {
                  'fragments': masque.fragments
                      .map((item) => item.toJson())
                      .toList(),
                }
        : {
            'fragments': state.profile.fragments
                .map((item) => item.toJson())
                .toList(),
          };
    final port = _port;
    final builder = state.builder;
    late final String sni;
    late final Map<String, dynamic> outbound;
    try {
      sni = _protocol == ScanProtocol.warp
          ? 'engage.cloudflareclient.com'
          : builder.scannerDomain(masque: _protocol.isMasque);
      outbound = builder.scannerOutbound(
        protocol: _protocol.wireName,
        mode: _mode,
        domain: sni,
        port: port,
      );
    } catch (error) {
      setState(() => _error = error.toString());
      return;
    }
    final options = <String, dynamic>{
      'protocol': _protocol.wireName,
      'mode': _mode,
      'sni': sni,
      'port': port,
      'cidrs': cidrs,
      'enableIpv6': state.profile.general.enableIpv6,
      'probeCount': probeCount,
      'outputCount': output,
      'attemptCount': _attemptCount,
      'attemptInterval': attemptInterval,
      'probeInterval': probeInterval,
      'outbound': outbound,
      'probeUrl': XrayConfigBuilder.probeUrl,
      ...modeData,
    };

    await _events?.cancel();
    setState(() {
      _error = null;
      _results = const [];
      _selectedResults.clear();
      _completed = 0;
      _total = 0;
      _resultPort = port;
      _phase = 'Starting…';
      _running = true;
      _paused = false;
    });
    _events = ScannerChannel.events.listen(
      _onEvent,
      onError: (Object error) {
        if (mounted) {
          setState(() {
            _error = error.toString();
            _running = false;
          });
        }
      },
    );
    try {
      await ScannerChannel.start(options);
    } catch (error) {
      await _events?.cancel();
      _events = null;
      if (mounted) {
        setState(() {
          _error = error.toString();
          _running = false;
        });
      }
    }
  }

  void _onEvent(ScannerEvent event) {
    if (!mounted) return;
    setState(() {
      switch (event.type) {
        case 'progress':
          _phase = 'Scanning';
          _completed = event.completed ?? _completed;
          _total = event.total ?? _total;
        case 'done':
        case 'stopped':
          _phase = event.type == 'done' ? 'Finished' : 'Stopped';
          _running = false;
          _paused = false;
          _results = event.results;
          _selectedResults = {for (final result in event.results) result.ip};
        case 'error':
          _phase = 'Failed';
          _running = false;
          _paused = false;
          _error = event.message ?? 'Scanner failed.';
          _results = event.results;
          _selectedResults = {for (final result in event.results) result.ip};
      }
    });
  }

  Future<void> _togglePause() async {
    try {
      if (_paused) {
        await ScannerChannel.resume();
      } else {
        await ScannerChannel.pause();
      }
      if (mounted) setState(() => _paused = !_paused);
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    }
  }

  Future<void> _stop() async {
    try {
      await ScannerChannel.stop();
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    }
  }

  void _install(AppState state) {
    final ips = _results
        .where((result) => _selectedResults.contains(result.ip))
        .map((result) => result.ip)
        .toList();
    if (ips.isEmpty) {
      setState(() => _error = 'Select at least one result to add.');
      return;
    }
    final added = state.installScannedIPs(
      ips,
      protocol: _protocol.isMasque ? 'masque' : _protocol.wireName,
      port: _resultPort!,
    );
    final skipped = ips.length - added;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(
          added == 0
              ? 'These IPs are already installed'
              : 'Added $added IP${added == 1 ? '' : 's'}${skipped > 0 ? ' ($skipped already there)' : ''}',
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final state = AppScope.of(context);
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final resultList = _results;
    final supported = ScannerChannel.isSupported;
    return Scaffold(
      appBar: AppBar(title: const Text('Cloudflare scanner')),
      body: ListView(
        padding: const EdgeInsets.only(bottom: 24),
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(24, 8, 24, 0),
            child: Text(
              'Measure real delay through a real '
              '${_protocol.isMasque
                  ? 'MASQUE'
                  : _protocol == ScanProtocol.warp
                  ? 'WARP'
                  : 'VLESS'} proxy for each candidate Cloudflare IP. '
              'Starting a scan disconnects the VPN.\n'
              'Scanner inherits all protocol settings from app like Fingerprint, IPv6, Fragment, UDP Obfs etc.',
              style: theme.textTheme.bodyMedium?.copyWith(
                color: scheme.onSurfaceVariant,
              ),
            ),
          ),
          if (!supported)
            const Padding(
              padding: EdgeInsets.fromLTRB(24, 16, 24, 0),
              child: Text('Scanning is currently available on Android.'),
            ),
          const SectionHeader('Scan settings'),
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
                  _DropdownSetting<String>(
                    label: 'Mode',
                    values: [
                      'normal',
                      _protocol == ScanProtocol.masqueH3 ||
                              _protocol == ScanProtocol.warp
                          ? 'noise'
                          : 'fragment',
                    ],
                    value: _mode,
                    itemLabel: (value) => switch (value) {
                      'normal' => 'Normal',
                      'fragment' => 'Fragment',
                      'noise' => 'Obfuscated',
                      _ => value,
                    },
                    enabled: !_running,
                    onChanged: (value) => setState(() => _mode = value),
                  ),
                  _DropdownSetting<int>(
                    label: 'Port',
                    value: _port,
                    values: _protocol.isWarpLike
                        ? ScannerAssets.warpPorts
                        : XrayConfigBuilder.httpsPorts,
                    enabled: !_running,
                    onChanged: (value) => setState(() => _port = value),
                  ),
                  _NumberInput(
                    controller: _probeCount,
                    label: 'Probes count',
                    enabled: !_running,
                  ),
                  _NumberInput(
                    controller: _probeInterval,
                    label: 'Probes interval (ms)',
                    enabled: !_running,
                  ),
                  _DropdownSetting<int>(
                    label: 'Attempts count',
                    value: _attemptCount,
                    values: const [1, 2, 3, 4, 5],
                    enabled: !_running,
                    onChanged: (value) => setState(() => _attemptCount = value),
                  ),
                  _NumberInput(
                    controller: _attemptInterval,
                    label: 'Attempts interval (ms)',
                    enabled: !_running,
                  ),
                  _NumberInput(
                    controller: _outputCount,
                    label: 'Outputs count',
                    enabled: !_running,
                  ),
                  SwitchListTile(
                    contentPadding: EdgeInsets.zero,
                    title: const Text('Custom CIDRs'),
                    value: _customCidrs,
                    onChanged: _running
                        ? null
                        : (value) => setState(() => _customCidrs = value),
                  ),
                  if (_customCidrs)
                    ListTile(
                      contentPadding: EdgeInsets.zero,
                      enabled: !_loadingCidrs && !_running,
                      title: Text('Select CIDRs'),
                      trailing: const Icon(Symbols.chevron_right_rounded),
                      subtitle: Text(
                        '${_selectedCidrs.length} of ${_cidrs.length} CIDRs selected',
                      ),
                      onTap: _loadingCidrs || _running ? null : _selectCidrs,
                    ),
                ],
              ),
            ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(12, 16, 12, 0),
            child: Wrap(
              alignment: WrapAlignment.center,
              spacing: 12,
              children: [
                FilledButton.icon(
                  icon: _loadingCidrs
                      ? const SizedBox(
                          width: 18,
                          height: 18,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : const Icon(Symbols.network_check_rounded),
                  label: Text(_loadingCidrs ? 'Loading CIDRs…' : 'Start scan'),
                  onPressed: _running || _loadingCidrs || !supported
                      ? null
                      : () => _start(state),
                ),
                if (_running)
                  OutlinedButton.icon(
                    icon: Icon(
                      _paused
                          ? Symbols.play_arrow_rounded
                          : Symbols.pause_rounded,
                    ),
                    label: Text(_paused ? 'Resume' : 'Pause'),
                    onPressed: _togglePause,
                  ),
                if (_running)
                  OutlinedButton.icon(
                    icon: const Icon(Symbols.stop_rounded),
                    label: const Text('Stop'),
                    onPressed: _stop,
                  ),
              ],
            ),
          ),
          if (_phase != null)
            ListTile(
              title: Text(_phase!),
              subtitle: _total > 0
                  ? Text('$_completed / $_total candidates')
                  : null,
              trailing: _running
                  ? const SizedBox(
                      width: 20,
                      height: 20,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : null,
            ),
          if (_error != null)
            Padding(
              padding: const EdgeInsets.fromLTRB(24, 12, 24, 0),
              child: Text(
                _error!,
                style: theme.textTheme.bodyMedium?.copyWith(
                  color: scheme.error,
                ),
              ),
            ),
          if (!_running &&
              (_phase == 'Finished' || _phase == 'Stopped') &&
              resultList.isEmpty)
            const Padding(
              padding: EdgeInsets.fromLTRB(24, 12, 24, 0),
              child: Text('No candidates passed all attempts.'),
            ),
          if (resultList.isNotEmpty) ...[
            SectionHeader('${resultList.length} fastest results'),
            SettingsCard(
              children: [
                for (final result in resultList)
                  CheckboxListTile(
                    value: _selectedResults.contains(result.ip),
                    onChanged: (value) => setState(
                      () => value == true
                          ? _selectedResults.add(result.ip)
                          : _selectedResults.remove(result.ip),
                    ),
                    title: Text(
                      result.ip,
                      style: const TextStyle(fontFamily: 'monospace'),
                    ),
                    subtitle: Text(
                      'Real delay ${result.httpMillis.toStringAsFixed(2)} ms',
                    ),
                  ),
              ],
            ),
            Padding(
              padding: const EdgeInsets.fromLTRB(12, 16, 12, 0),
              child: FilledButton.icon(
                icon: const Icon(Symbols.add_circle_rounded),
                label: Text(
                  _protocol == ScanProtocol.warp
                      ? 'Add to endpoints'
                      : _protocol.isMasque
                      ? 'Add to endpoints'
                      : 'Add to clean IPs',
                ),
                onPressed: () => _install(state),
              ),
            ),
          ],
          if (!_loadingCidrs && _cidrs.isEmpty)
            const Padding(
              padding: EdgeInsets.all(24),
              child: Text(
                'No CIDR asset is available. Rebuild the app after fetching scanner CIDRs.',
              ),
            ),
        ],
      ),
    );
  }
}

class _NumberInput extends StatelessWidget {
  final TextEditingController controller;
  final String label;
  final bool enabled;

  const _NumberInput({
    required this.controller,
    required this.label,
    required this.enabled,
  });

  @override
  Widget build(BuildContext context) => TextField(
    controller: controller,
    enabled: enabled,
    keyboardType: TextInputType.number,
    decoration: InputDecoration(labelText: label),
  );
}

class _DropdownSetting<T> extends StatelessWidget {
  final String label;
  final T value;
  final List<T> values;
  final bool enabled;
  final ValueChanged<T> onChanged;
  final String Function(T)? itemLabel;

  const _DropdownSetting({
    required this.label,
    required this.value,
    required this.values,
    required this.enabled,
    required this.onChanged,
    this.itemLabel,
  });

  @override
  Widget build(BuildContext context) => DropdownButtonFormField<T>(
    initialValue: value,
    decoration: InputDecoration(labelText: label, enabled: enabled),
    items: [
      for (final item in values)
        DropdownMenuItem(
          value: item,
          child: Text(itemLabel?.call(item) ?? '$item'),
        ),
    ],
    onChanged: enabled
        ? (value) {
            if (value != null) onChanged(value);
          }
        : null,
  );
}
