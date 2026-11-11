import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:material_symbols_icons/symbols.dart';
import 'package:qr_flutter/qr_flutter.dart';

import 'prefs.dart';

const String _usdtBep20Address = '0xbdf15d41C56f861f25b2b11C835bd45dfD5b792F';
const String _usdtTrc20Address = 'TSEi338sydN11xh8E7ZLPUL5qc8sQtt8G4';
const String _usdtTonAddress = 'UQCHVokmmzsShpLL3FEUuLcnb9Bb6IKXWwEhJcfpr50EqMUa';

class DonationPage extends StatelessWidget {
  const DonationPage({
    super.key,
    this.bep20Address = _usdtBep20Address,
    this.trc20Address = _usdtTrc20Address,
    this.tonAddress = _usdtTonAddress,
  });

  final String? bep20Address;
  final String? trc20Address;
  final String? tonAddress;

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('Donation')),
    body: ListView(
      padding: const EdgeInsets.symmetric(vertical: 8),
      children: [
        SettingsCard(
          children: [
            _DonationTile(
              name: 'Tether',
              network: 'BEP-20',
              address: bep20Address,
            ),
            _DonationTile(
              name: 'Tether',
              network: 'TRC-20',
              address: trc20Address,
            ),
            _DonationTile(
              name: 'Tether',
              network: 'TON',
              address: tonAddress,
            ),
          ],
        ),
      ],
    ),
  );
}

class _DonationTile extends StatelessWidget {
  const _DonationTile({
    required this.name,
    required this.network,
    required this.address,
  });

  final String name;
  final String network;
  final String? address;

  Future<void> _copy(BuildContext context, String address) async {
    await Clipboard.setData(ClipboardData(text: address));
    if (!context.mounted) return;
    ScaffoldMessenger.of(context)
        .showSnackBar(SnackBar(content: Text('$name $network address copied')));
  }

  void _showQr(BuildContext context, String address) {
    showDialog<void>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text('$name · $network'),
        scrollable: true,
        content: Column(
          mainAxisSize: MainAxisSize.min,
          spacing: 20,
          children: [
            SizedBox(
              width: MediaQuery.sizeOf(context).width,
              child: AspectRatio(
                aspectRatio: 1,
                // QrImageView uses LayoutBuilder, which cannot report intrinsic sizes.
                child: SizedBox.square(
                  dimension: MediaQuery.sizeOf(context).width,
                  child: ClipRRect(
                    borderRadius: BorderRadius.circular(16),
                    child: QrImageView(
                      data: address,
                      backgroundColor: Colors.white,
                      padding: const EdgeInsets.all(16),
                    ),
                  ),
                ),
              ),
            ),
            // const SizedBox(height: 12),
            SelectableText(address),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(),
            child: const Text('Close'),
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final value = address?.trim();
    final configuredAddress = value == null || value.isEmpty ? null : value;
    return ListTile(
      title: Text('$name · $network'),
      subtitle: Text(
        configuredAddress ?? 'Address not configured',
        overflow: TextOverflow.ellipsis,
      ),
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          IconButton(
            tooltip: 'Copy $network address',
            icon: const Icon(Symbols.content_copy_rounded),
            onPressed: configuredAddress == null
                ? null
                : () => _copy(context, configuredAddress),
          ),
          IconButton(
            tooltip: '$network QR code',
            icon: const Icon(Symbols.qr_code_rounded),
            onPressed: configuredAddress == null
                ? null
                : () => _showQr(context, configuredAddress),
          ),
        ],
      ),
    );
  }
}
