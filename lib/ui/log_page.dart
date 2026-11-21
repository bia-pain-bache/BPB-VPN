import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:material_symbols_icons/symbols.dart';

import '../services/vpn_channel.dart';
import '../state/app_state.dart';
import 'chip_menu.dart';

/// The logs of the current (or last) connection, refreshed live: Xray's (VPN), and Tor's /
/// Psiphon's when they ran, each as its program wrote it. Also the Xray log level.
class LogPage extends StatefulWidget {
  const LogPage({super.key});

  @override
  State<LogPage> createState() => _LogPageState();
}

class _LogPageState extends State<LogPage> {
  static const _levels = {'none': 'None', 'error': 'Error', 'warning': 'Warning', 'info': 'Info', 'debug': 'Debug'};

  /// The logs, by the name the service uses.
  static const _logs = {'xray': 'VPN', 'tor': 'Tor', 'psiphon': 'Psiphon'};

  /// Most recent lines kept on screen.
  static const _maxLines = 1000;

  final _scroll = ScrollController();
  final _searchController = TextEditingController();
  Timer? _poll;
  List<String> _lines = const [];

  /// The log on screen, and the logs with something in them (Xray's always shown).
  String _log = 'xray';
  List<String> _available = const ['xray'];

  /// Keep scrolling to new lines while the view is at the bottom.
  bool _follow = true;

  /// Search visibility and query.
  bool _searching = false;
  String _searchQuery = '';

  @override
  void initState() {
    super.initState();
    _scroll.addListener(() {
      if (!_scroll.hasClients) return;
      _follow = _scroll.position.pixels <= 24;
    });
    _load();
    _poll = Timer.periodic(const Duration(seconds: 1), (_) => _load());
  }

  @override
  void dispose() {
    _poll?.cancel();
    _scroll.dispose();
    _searchController.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    final String text;
    final Map<String, int> sizes;
    final log = _log;
    try {
      sizes = await VpnChannel.logSizes();
      text = await VpnChannel.readLog(log: log);
    } catch (_) {
      return;
    }
    if (!mounted || log != _log) return;
    final available = _logs.keys.where((l) => l == 'xray' || (sizes[l] ?? 0) > 0).toList();
    if (available.join() != _available.join()) setState(() => _available = available);
    final tail = text.length > 200000 ? text.substring(text.length - 200000) : text;
    var lines = tail.split('\n').where((l) => l.trim().isNotEmpty).toList();
    if (lines.length > _maxLines) lines = lines.sublist(lines.length - _maxLines);
    if (lines.length == _lines.length && (lines.isEmpty || lines.last == _lines.last)) return;
    setState(() => _lines = lines);
  }

  Future<void> _copy() async {
    final messenger = ScaffoldMessenger.of(context);
    final filtered = _lines.where((l) => _searchQuery.isEmpty || l.toLowerCase().contains(_searchQuery)).toList();
    await Clipboard.setData(ClipboardData(text: filtered.join('\n')));
    messenger.showSnackBar(const SnackBar(content: Text('Log copied')));
  }

  Future<void> _clear() async {
    await VpnChannel.clearLog();
    if (mounted) {
      setState(() {
        _lines = const [];
        _log = 'xray';
        _available = const ['xray'];
        _searching = false;
        _searchQuery = '';
        _searchController.clear();
      });
    }
  }

  void _show(String log) {
    if (log == _log) return;
    setState(() {
      _log = log;
      _lines = const [];
      _follow = true;
      _searching = false;
      _searchQuery = '';
      _searchController.clear();
    });
    _load();
  }

  /// Colour by level as each program writes it: Xray "[Warning]", Tor "[warn]", Psiphon
  /// "noticeType":"Warning"; and the app's own "[App]" lines.
  Color? _colorFor(String line, ColorScheme scheme) {
    if (line.contains('[Error]') || line.contains('[err]') || line.contains('"noticeType":"Error"')) {
      return scheme.error;
    }
    if (line.contains('[Warning]') ||
        line.contains('[warn]') ||
        line.contains('"noticeType":"Warning"') ||
        line.contains('"noticeType":"Alert"')) {
      return scheme.tertiary;
    }
    if (line.contains('[App]')) return scheme.primary;
    if (line.contains('[Debug]')) return scheme.onSurfaceVariant;
    return null;
  }

  @override
  Widget build(BuildContext context) {
    final state = AppScope.of(context);
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final level = state.device.logLevel;

    final filteredLines = _lines.where((l) => _searchQuery.isEmpty || l.toLowerCase().contains(_searchQuery)).toList();

    return Scaffold(
      appBar: AppBar(
        title: const Text('Logs'),
        actions: [
          IconButton(
            tooltip: _searching ? 'Close search' : 'Search',
            onPressed: _lines.isEmpty && !_searching ? null : () {
              setState(() {
                _searching = !_searching;
                if (!_searching) {
                  _searchQuery = '';
                  _searchController.clear();
                }
              });
            },
            icon: Icon(_searching ? Symbols.search_off_rounded : Symbols.search_rounded),
          ),
          IconButton(
            tooltip: 'Copy',
            onPressed: _lines.isEmpty ? null : _copy,
            icon: const Icon(Symbols.content_copy_rounded),
          ),
          IconButton(
            tooltip: 'Clear',
            onPressed: _lines.isEmpty ? null : _clear,
            icon: const Icon(Symbols.delete_rounded),
          ),
        ],
      ),
      body: Column(
        children: [
          ListTile(
            leading: const Icon(Symbols.tune_rounded),
            title: const Text('Log level'),
            subtitle: Text(state.isConnected ? 'Reconnect after changes.' : 'Applies to the next connection.'),
            trailing: ChipMenu(
              values: _levels.keys.toList(),
              selected: level,
              label: (v) => _levels[v] ?? v,
              onSelected: (v) => state.write('device.logLevel', v),
            ),
          ),
          if (_available.length > 1)
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 8),
              child: SegmentedButton<String>(
                segments: [for (final l in _available) ButtonSegment(value: l, label: Text(_logs[l]!))],
                selected: {_log},
                showSelectedIcon: false,
                onSelectionChanged: (s) => _show(s.first),
              ),
            ),
          if (_searching)
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 0, 16, 10),
              child: TextField(
                controller: _searchController,
                autofocus: true,
                decoration: InputDecoration(
                  hintText: 'Search logs',
                  prefixIcon: const Icon(Symbols.search_rounded, size: 22,),
                  suffixIcon: _searchQuery.isNotEmpty
                      ? IconButton(
                          icon: const Icon(Symbols.clear_rounded),
                          onPressed: () {
                            setState(() {
                              _searchQuery = '';
                              _searchController.clear();
                            });
                          },
                        )
                      : null,
                  isDense: true,
                ),
                onChanged: (v) => setState(() => _searchQuery = v.trim().toLowerCase()),
              ),
            ),
          const Divider(height: 1),
          Expanded(
            child: _lines.isEmpty
                ? Center(
                    child: Padding(
                      padding: const EdgeInsets.all(32),
                      child: Text(
                        _log != 'xray'
                            ? 'Nothing logged yet.'
                            : level == 'none'
                            ? 'Logging is off. Choose a log level, then connect.'
                            : 'No log yet. Connect to see what Xray reports.',
                        textAlign: TextAlign.center,
                        style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant),
                      ),
                    ),
                  )
                : filteredLines.isEmpty
                    ? Center(
                        child: Padding(
                          padding: const EdgeInsets.all(32),
                          child: Text(
                            'No matching log lines found.',
                            textAlign: TextAlign.center,
                            style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant),
                          ),
                        ),
                      )
                    : ListView.separated(
                        controller: _scroll,
                        reverse: true,
                        padding: const EdgeInsets.fromLTRB(12, 8, 12, 24),
                        itemCount: filteredLines.length,
                        separatorBuilder: (context, index) => Padding(
                          padding: const EdgeInsets.symmetric(vertical: 4),
                          child: Text(
                            '-' * 400,
                            style: TextStyle(
                              fontFamily: 'monospace',
                              fontSize: 12,
                              height: 1.0,
                              color: scheme.outlineVariant.withValues(alpha: 0.9),
                            ),
                            maxLines: 1,
                            overflow: TextOverflow.clip,
                          ),
                        ),
                        itemBuilder: (context, i) {
                          final line = filteredLines[filteredLines.length - 1 - i];
                          return Text(
                            line,
                            style: TextStyle(
                              fontFamily: 'monospace',
                              fontSize: 12,
                              height: 1.35,
                              color: _colorFor(line, scheme),
                            ),
                          );
                        },
                      ),
          ),
        ],
      ),
    );
  }
}