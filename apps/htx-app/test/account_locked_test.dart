import 'package:agrigreen_htx/ui/providers.dart';
import 'package:agrigreen_htx/ui/screens/home_screen.dart';
import 'package:agrigreen_htx/ui/screens/login_screen.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/data/sync/sync_service.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/test_env.dart';

void main() {
  // FN-01/EX-06 (AH-301): tài khoản bị khóa trong lúc đang dùng.
  testWidgets(
    'AC-N2: lượt đồng bộ bị từ chối 403 accounts.account_locked → về màn đăng '
    'nhập kèm câu EX-03, token bị xóa, ảnh chờ gửi không được gửi đi',
    (tester) async {
      final env = TestEnv(roles: const ['coop_manager'])..seed();
      final s = await loginToHome(tester, env);
      final shot = await s.photos.capture(
        subjectRef: householdRef,
        parentEntity: 'farm_log',
      );
      expect(shot, isNotNull);
      env.server.accountLocked = true;
      await s.sync.syncOnce(force: true);
      await settle(tester, 30);

      expect(find.byType(LoginScreen), findsOneWidget);
      expect(find.byType(HomeScreen), findsNothing);
      expect(find.text(AccountLockedException.userMessage), findsOneWidget);
      expect(await env.store.read('ag.oidc.access'), isNull);
      expect(env.server.files, isEmpty);
      await finish(tester);
    },
  );

  // CSDL của môi trường thử ở bộ nhớ nên mất khi app đóng services; việc giữ
  // và gửi tiếp dữ liệu chờ sau mở khóa kiểm ở
  // packages/agrigreen_mobile/test/account_locked_sync_test.dart (SQLite thật).
  testWidgets('mở khóa → đăng nhập lại, vào trang chủ và đồng bộ bình thường', (
    tester,
  ) async {
    final env = TestEnv(roles: const ['coop_manager'])..seed();
    final s = await loginToHome(tester, env);
    env.server.accountLocked = true;
    await s.sync.syncOnce(force: true);
    await settle(tester, 30);
    expect(find.byType(LoginScreen), findsOneWidget);

    env.server.accountLocked = false;
    await tester.tap(find.text('Đăng nhập'));
    await settle(tester, 30);
    expect(find.byType(HomeScreen), findsOneWidget);
    final again = containerOf(tester).read(appControllerProvider).services!;
    final shot = await again.photos.capture(
      subjectRef: householdRef,
      parentEntity: 'farm_log',
    );
    expect(shot, isNotNull);
    expect(await again.sync.syncOnce(force: true), isNotNull);
    final st = await again.sync.status();
    expect(st.state, SyncState.idle);
    expect(st.lastError, isNull);
    expect(env.server.files, hasLength(1));
    expect(await again.evidence.pending(), isEmpty);
    await finish(tester);
  });
}
