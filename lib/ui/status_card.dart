import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:material_symbols_icons/symbols.dart';

import '../models/settings.dart';
import '../services/vpn_channel.dart';
import '../state/app_state.dart';
import 'format.dart';
import 'theme.dart';

/// Connected colour; stays green whatever theme colour is picked.
const _green = Color(0xFF1FA463);
// const _tabular = TextStyle(fontFeatures: [FontFeature.tabularFigures()]);

// Layout constants: the power button is positioned from these.
const _pad = 24.0;
const _rowHeight = 124.0;
const _button = 116.0;

/// Home status card: state, power button, duration, ping and exit IPs (the connection's
/// settings are on the ConnectionCard above it).
class StatusCard extends StatefulWidget {
  const StatusCard({super.key});

  @override
  State<StatusCard> createState() => _StatusCardState();
}

class _StatusCardState extends State<StatusCard> {
  /// Ticks the connection duration.
  Timer? _ticker;

  @override
  void initState() {
    super.initState();
    _ticker = Timer.periodic(const Duration(seconds: 1), (_) {
      if (mounted && AppScope.read(context).isConnected) setState(() {});
    });
  }

  @override
  void dispose() {
    _ticker?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final state = AppScope.of(context);
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final failed =
        state.status == VpnStatus.disconnected && state.error != null;
    final (title, subtitle) = switch (state.status) {
      // Psiphon / Tor say what they're doing: progress while connecting, or reconnecting.
      VpnStatus.connected => (
        'CONNECTED',
        state.exitDetail ?? 'Tap to disconnect',
      ),
      VpnStatus.connecting => (
        state.connectedAt == null ? 'CONNECTING' : 'RECONNECTING',
        state.exitDetail ?? 'Starting the tunnel…',
      ),
      VpnStatus.disconnecting => ('DISCONNECTING', 'Stopping the tunnel…'),
      VpnStatus.disconnected when failed => ('Connection failed', state.error!),
      VpnStatus.disconnected => ('DISCONNECTED', 'Tap to connect'),
    };
    final titleColor = state.isConnected
        ? _green
        : (failed ? scheme.error : scheme.onSurface);
    final muted = scheme.onSurfaceVariant;

    return Card(
      margin: const EdgeInsets.symmetric(horizontal: 12),
      color: scheme.surfaceContainer,
      elevation: 0,
      shape: roundedShape(
        kHeroRadius,
        side: BorderSide(
          color: scheme.outlineVariant.withValues(alpha: 0.75),
          width: 0.75,
        ),
      ),
      child: Stack(
        // Let the button's shadow spill past the card edge instead of being cut off.
        clipBehavior: Clip.none,
        children: [
          Padding(
            padding: const EdgeInsets.all(_pad),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                // Title and hint on the left; space for the button on the right.
                ConstrainedBox(
                  constraints: const BoxConstraints(minHeight: _rowHeight),
                  child: Row(
                    children: [
                      Expanded(
                        child: Column(
                          mainAxisAlignment: MainAxisAlignment.center,
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            FittedBox(
                              fit: BoxFit.scaleDown,
                              alignment: AlignmentDirectional.centerStart,
                              child: Text(
                                title,
                                style: theme.textTheme.titleLarge?.copyWith(
                                  fontWeight: FontWeight.w700,
                                  color: titleColor,
                                ),
                              ),
                            ),
                            const SizedBox(height: 4),
                            Text(
                              subtitle,
                              style: theme.textTheme.bodyMedium?.copyWith(
                                color: muted,
                              ),
                            ),
                            if (state.isConnected &&
                                state.connectedAt != null) ...[
                              const SizedBox(height: 4),
                              Row(
                                mainAxisSize: MainAxisSize.min,
                                children: [
                                  Icon(
                                    Symbols.timer_rounded,
                                    size: 18,
                                    color: muted,
                                  ),
                                  const SizedBox(width: 5),
                                  Text(
                                    formatDuration(
                                      DateTime.now().difference(
                                        state.connectedAt!,
                                      ),
                                    ),
                                    style: theme.textTheme.titleMedium,
                                  ),
                                ],
                              ),
                            ],
                          ],
                        ),
                      ),
                      const SizedBox(width: 16 + _button),
                    ],
                  ),
                ),
                const SizedBox(height: 16),
                _Metrics(),
              ],
            ),
          ),
          Positioned(
            right: _pad - 8,
            top: _pad - 8,
            width: _button + 16,
            height: _button + 16,
            child: const _PowerButton(),
          ),
        ],
      ),
    );
  }
}

/// Duration, ping and the exit IPs in a vertical list.
class _Metrics extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    final state = AppScope.of(context);
    final ips = state.myIps;
    final connected = state.isConnected;

    Widget ip(String? info) {
      final text = ips == null
          ? (state.loadingIps ? '…' : '—')
          : info ?? (state.loadingIps ? '…' : 'Unavailable');
      return Text(text, maxLines: 1, overflow: TextOverflow.ellipsis);
    }

    final refreshIps = state.refreshMyIps;
    final twoIps = state.mode.needsWorker && state.exitNode == ExitNode.worker;
    // This phone's LAN address (always shown); with LAN connections allowed, other
    // devices use it with port 10808 for the SOCKS/HTTP proxy.
    final lanIp = state.lanIp;
    final proxy = lanIp != null && state.device.lanSharing ? '$lanIp:10808' : 'Disabled';
    final lan = _Metric(
      icon: Symbols.network_node_rounded,
      label: 'Share proxy',
      value: Text(lanIp != null ? proxy : '—', maxLines: 1, overflow: TextOverflow.ellipsis),
      tooltip: 'Tap to copy',
      onTap: lanIp == null
          ? null
          : () {
              Clipboard.setData(ClipboardData(text: proxy));
              ScaffoldMessenger.of(context)
                  .showSnackBar(SnackBar(content: Text('Copied $proxy')));
            },
    );

    // When disconnected, show local ISP IP/ISP.
    final showLocal = !connected && ips?.hasLocal == true;
    final localIp = ips?.local?.ip;
    final localIsp = ips?.local?.isp;

    final metrics = <Widget>[
      _Metric(
        icon: Symbols.speed_rounded,
        label: 'Ping',
        value: connected ? const _PingButton() : const Text('—'),
      ),
      // Through the worker alone, Cloudflare-hosted sites see the proxy IP and others the
      // worker's exit IP. WARP, Serverless and the chain / Psiphon exits have a single exit IP.
      _Metric(
        icon: Symbols.location_on_rounded,
        label: 'My IP',
        value: ip(showLocal ? localIp : (ips?.other?.ip ?? (twoIps ? null : ips?.cloudflare?.ip))),
        onTap: refreshIps,
      ),
      _Metric(
        icon: Symbols.target,
        label: 'ISP',
        value: ip(showLocal ? localIsp : ips?.other?.isp),
        onTap: refreshIps,
      ),
    ];
    if (twoIps && connected) {
      metrics.add(
        _Metric(
          icon: Symbols.location_on_rounded,
          label: 'My IP Cloudflare',
          value: ip(ips?.cloudflare?.ip),
          onTap: refreshIps,
        ),
      );
    }
    metrics.add(lan);

    return Column(
      spacing: 10,
      children: [
        for (var i = 0; i < metrics.length; i++) ...[metrics[i]],
      ],
    );
  }
}

class _Metric extends StatelessWidget {
  final IconData icon;
  final String label;
  final Widget value;

  /// Tap action (refresh the IPs, copy the proxy address) and its hint.
  final VoidCallback? onTap;
  final String tooltip;

  const _Metric({
    required this.icon,
    required this.label,
    required this.value,
    this.onTap,
    this.tooltip = 'Tap to refresh',
  });

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final content = Row(
      children: [
        Expanded(
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(
                icon,
                size: 18,
                color: theme.colorScheme.onSurfaceVariant,
              ),
              const SizedBox(width: 6),
              Flexible(
                child: Text(
                  label,
                  style: theme.textTheme.titleMedium?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
            ],
          ),
        ),
        const SizedBox(width: 16),
        Expanded(
          child: Align(
            alignment: AlignmentDirectional.centerEnd,
            child: DefaultTextStyle.merge(
              style: theme.textTheme.titleMedium,
              child: SizedBox(
                height: 28,
                child: Align(
                  alignment: AlignmentDirectional.centerEnd,
                  child: value,
                ),
              ),
            ),
          ),
        ),
      ],
    );
    final padded = Padding(
      padding: const EdgeInsets.symmetric(vertical: 2),
      child: content,
    );
    if (onTap == null) return padded;
    return Tooltip(
      message: tooltip,
      child: InkWell(
        borderRadius: BorderRadius.circular(8),
        onTap: onTap,
        child: padded,
      ),
    );
  }
}

/// Bold bolt that tests the ping; shows the result next to it once measured.
class _PingButton extends StatelessWidget {
  const _PingButton();

  @override
  Widget build(BuildContext context) {
    final state = AppScope.of(context);
    final theme = Theme.of(context);
    final ping = state.pingMs;
    final color = theme.colorScheme.primary;
    return Tooltip(
      message: 'Test ping',
      child: InkWell(
        borderRadius: BorderRadius.circular(16),
        onTap: state.pinging ? null : state.testPing,
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 2),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              if (state.pinging)
                SizedBox(
                  width: 22,
                  height: 22,
                  child: CircularProgressIndicator(
                    strokeWidth: 2.5,
                    color: color,
                  ),
                )
              else
                Icon(
                  Symbols.electric_bolt_rounded,
                  size: 18,
                  weight: 700,
                  color: color,
                ),
              const SizedBox(width: 6),
              Text(
                state.pinging
                    ? 'Testing'
                    : ping == null
                    ? 'Test'
                    : (ping < 0 ? 'Timeout' : '$ping ms'),
                style: TextStyle(
                  color: ping != null && ping < 0
                      ? theme.colorScheme.error
                      : color,
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// Big round power button. The icon points down while disconnected and flips up when
/// connecting/connected; it glows when connected and shows a spinning ring while busy.
class _PowerButton extends StatefulWidget {
  const _PowerButton();

  @override
  State<_PowerButton> createState() => _PowerButtonState();
}

class _PowerButtonState extends State<_PowerButton> {
  bool _pressed = false;

  /// Icon rotation in turns. It only ever grows by half a turn, so the icon always
  /// spins clockwise: down → up on connect, up → down on disconnect.
  double? _turns;
  bool? _up;

  @override
  Widget build(BuildContext context) {
    final state = AppScope.of(context);
    final scheme = Theme.of(context).colorScheme;
    final busy = state.isBusy;
    final (Color fill, Color iconColor) = switch (state.status) {
      VpnStatus.connected => (_green, Colors.white),
      VpnStatus.disconnected => (
        scheme.primaryContainer,
        scheme.onPrimaryContainer,
      ),
      _ => (scheme.primary, scheme.onPrimary),
    };
    final up =
        state.status == VpnStatus.connected ||
        state.status == VpnStatus.connecting;
    if (_up == null) {
      _turns = up ? 0 : 0.5;
    } else if (_up != up) {
      _turns = _turns! + 0.5;
    }
    _up = up;
    const duration = Duration(milliseconds: 500);

    return Semantics(
      button: true,
      label: state.status == VpnStatus.disconnected ? 'Connect' : 'Disconnect',
      child: Stack(
        alignment: Alignment.center,
        children: [
          if (busy)
            Positioned.fill(
              child: CircularProgressIndicator(
                strokeWidth: 3,
                strokeCap: StrokeCap.round,
                color: fill,
              ),
            ),
          AnimatedScale(
            scale: _pressed ? 0.92 : 1,
            duration: const Duration(milliseconds: 150),
            curve: Curves.easeOut,
            child: AnimatedContainer(
              duration: duration,
              curve: Curves.easeOutCubic,
              width: _button,
              height: _button,
              decoration: BoxDecoration(
                shape: BoxShape.circle,
                // A slightly lighter top-left gives the button some depth.
                gradient: LinearGradient(
                  begin: Alignment.topLeft,
                  end: Alignment.bottomRight,
                  colors: [Color.lerp(fill, Colors.white, 0.18)!, fill],
                ),
                boxShadow: [
                  // Coloured glow, stronger when connected.
                  BoxShadow(
                    color: fill.withValues(
                      alpha: state.isConnected ? 0.5 : 0.3,
                    ),
                    blurRadius: state.isConnected ? 32 : 24,
                    offset: const Offset(0, 10),
                  ),
                  // Tight contact shadow under the button.
                  BoxShadow(
                    color: Colors.black.withValues(alpha: 0.25),
                    blurRadius: 8,
                    offset: const Offset(0, 4),
                  ),
                ],
              ),
              child: Material(
                type: MaterialType.transparency,
                shape: const CircleBorder(),
                clipBehavior: Clip.antiAlias,
                child: InkWell(
                  // Always tappable: while connecting (Psiphon may search for a long time) it cancels.
                  onTap: state.toggle,
                  onHighlightChanged: (v) => setState(() => _pressed = v),
                  child: Center(
                    child: AnimatedRotation(
                      turns: _turns!,
                      duration: const Duration(milliseconds: 700),
                      curve: Curves.easeOutBack,
                      child: TweenAnimationBuilder<Color?>(
                        tween: ColorTween(end: iconColor),
                        duration: duration,
                        builder: (context, color, _) => Icon(
                          Symbols.power_settings_new_rounded,
                          size: 56,
                          weight: 600,
                          color: color,
                        ),
                      ),
                    ),
                  ),
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}
