import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_highlight/flutter_highlight.dart';
import 'package:flutter_highlight/themes/tomorrow-night-bright.dart';
import 'package:material_symbols_icons/symbols.dart';

import '../services/vpn_channel.dart';
import '../state/app_state.dart';

/// Shows the generated Xray config for the current connection mode.
class ConfigViewPage extends StatefulWidget {
  const ConfigViewPage({super.key});

  @override
  State<ConfigViewPage> createState() => _ConfigViewPageState();
}

class _ConfigViewPageState extends State<ConfigViewPage> {
  Future<String>? _config;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _config ??= AppScope.of(context).buildConfig().then(
      (build) => const JsonEncoder.withIndent('  ').convert(build.config),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Configuration'),
        actions: [
          IconButton(
            tooltip: 'Download',
            icon: const Icon(Symbols.download_rounded),
            onPressed: () async {
              final text = await _config;
              if (text == null || !context.mounted) return;
              try {
                final where = await VpnChannel.saveFile(
                  name: 'bpb-config.json',
                  content: text,
                );
                if (!context.mounted) return;
                if (where != null) {
                  ScaffoldMessenger.of(context).showSnackBar(
                    SnackBar(content: Text('Saved to $where')),
                  );
                }
              } on PlatformException catch (e) {
                if (!context.mounted) return;
                ScaffoldMessenger.of(context).showSnackBar(
                  SnackBar(content: Text("Couldn't download: ${e.message}")),
                );
              }
            },
          ),
          IconButton(
            tooltip: 'Copy',
            icon: const Icon(Symbols.content_copy_rounded),
            onPressed: () async {
              final text = await _config;
              if (text == null || !context.mounted) return;
              await Clipboard.setData(ClipboardData(text: text));
              if (!context.mounted) return;
              ScaffoldMessenger.of(context)
                  .showSnackBar(const SnackBar(content: Text('Config copied')));
            },
          ),
        ],
      ),
      body: FutureBuilder<String>(
        future: _config,
        builder: (context, snapshot) {
          if (snapshot.hasError) {
            return Center(
              child: Padding(
                padding: const EdgeInsets.all(24),
                child: Text('${snapshot.error}'),
              ),
            );
          }
          if (!snapshot.hasData) {
            return const Center(child: CircularProgressIndicator());
          }
          return SingleChildScrollView(
            child: Container(
              width: double.infinity,
              color: Colors.black,
              child: HighlightView(
                snapshot.data!,
                language: 'json',
                theme: tomorrowNightBrightTheme,
                padding: const EdgeInsets.all(16),
                textStyle: const TextStyle(
                  fontFamily: 'monospace',
                  fontSize: 12,
                  height: 1.5,
                ),
              ),
            ),
          );
        },
      ),
    );
  }
}
