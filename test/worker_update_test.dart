import 'package:bpb/models/cf_account.dart';
import 'package:bpb/models/settings.dart';
import 'package:bpb/services/cloudflare_api.dart';
import 'package:bpb/services/vpn_channel.dart';
import 'package:bpb/state/app_state.dart';
import 'package:bpb/ui/cf_accounts_page.dart';
import 'package:bpb/ui/home_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

SavedCfAccount _account({required int version}) => SavedCfAccount(
  id: 'acc-1',
  name: 'Main',
  token: 'cfut_${'a' * 60}',
  email: 'me@example.com',
  uuid: '11111111-1111-1111-1111-111111111111',
  deployments: [
    Deployment(
      type: WorkerType.workers,
      name: 'wk-1',
      host: 'wk-1.me.workers.dev',
      version: version,
    ),
    Deployment(
      type: WorkerType.pages,
      name: 'pg-1',
      host: 'pg-1.pages.dev',
      version: version,
    ),
  ],
);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  void mockCoreChannel(WidgetTester tester) {
    const channel = MethodChannel('bpb/core');
    final messenger = tester.binding.defaultBinaryMessenger;
    messenger.setMockMethodCallHandler(channel, (call) async {
      if (call.method == 'prepare') return true;
      return null;
    });
    addTearDown(() => messenger.setMockMethodCallHandler(channel, null));
  }

  testWidgets('connect fails with error when VLESS worker is below minWorkerVersion', (
    tester,
  ) async {
    mockCoreChannel(tester);
    final state = AppState()
      ..cfAccounts = [_account(version: CloudflareApi.minWorkerVersion - 1)];
    addTearDown(state.dispose);

    await state.connect();
    expect(state.status, VpnStatus.disconnected);
    expect(
      state.error,
      'Update the worker first: Settings → Cloudflare accounts.',
    );
  });

  testWidgets('connect includes worker update failure reason when present', (
    tester,
  ) async {
    mockCoreChannel(tester);
    final acc = _account(version: CloudflareApi.minWorkerVersion - 1);
    final state = AppState()..cfAccounts = [acc];
    state.deployErrors[AppState.deploymentKey(acc, acc.deployments.first)] =
        "Can't reach Cloudflare.";
    addTearDown(state.dispose);

    await state.connect();
    expect(state.status, VpnStatus.disconnected);
    expect(
      state.error,
      "Worker update failed (Can't reach Cloudflare.). Update it first: Settings → Cloudflare accounts.",
    );
  });

  testWidgets('HomePage shows banner when background worker update fails', (
    tester,
  ) async {
    final acc = _account(version: CloudflareApi.minWorkerVersion - 1);
    final state = AppState()..cfAccounts = [acc];
    state.deployErrors[AppState.deploymentKey(acc, acc.deployments.first)] =
        "Can't reach Cloudflare.";
    addTearDown(state.dispose);

    await tester.pumpWidget(
      AppScope(
        state: state,
        child: const MaterialApp(home: HomePage()),
      ),
    );

    expect(find.text('Worker update failed'), findsOneWidget);
    expect(find.text("Can't reach Cloudflare."), findsOneWidget);
    expect(find.text('Retry'), findsOneWidget);
  });

  testWidgets('HomePage hides worker banner when up to date without errors', (
    tester,
  ) async {
    final acc = _account(version: CloudflareApi.minWorkerVersion);
    final state = AppState()..cfAccounts = [acc];
    addTearDown(state.dispose);

    await tester.pumpWidget(
      AppScope(
        state: state,
        child: const MaterialApp(home: HomePage()),
      ),
    );

    expect(find.text('Worker update failed'), findsNothing);
    expect(find.text('Worker update required'), findsNothing);
  });

  testWidgets('CfAccountsPage displays deploy errors on account tile', (
    tester,
  ) async {
    final acc = _account(version: CloudflareApi.minWorkerVersion - 1);
    final state = AppState()..cfAccounts = [acc];
    state.deployErrors[AppState.deploymentKey(acc, acc.deployments.first)] =
        'Authentication error';
    addTearDown(state.dispose);

    await tester.pumpWidget(
      AppScope(
        state: state,
        child: const MaterialApp(home: CfAccountsPage()),
      ),
    );

    expect(find.text('Workers: Authentication error'), findsOneWidget);
    expect(find.text('Update'), findsOneWidget);
  });
}
