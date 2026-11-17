import 'package:agrigreen_htx/ui/screens/access_blocked_screen.dart';
import 'package:agrigreen_htx/ui/screens/data_consent_screen.dart';
import 'package:agrigreen_htx/ui/screens/home_screen.dart';
import 'package:agrigreen_htx/ui/screens/login_screen.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/test_env.dart';

/// Màn chặn hiện, không màn nghiệp vụ nào, không đồng bộ (AH-303).
void _expectBlocked(TestEnv env, String message) {
  expect(find.byType(AccessBlockedScreen), findsOneWidget);
  expect(find.text(message), findsOneWidget);
  expect(find.byType(HomeScreen), findsNothing);
  expect(find.byType(DataConsentScreen), findsNothing);
  expect(find.byType(BigTile, skipOffstage: false), findsNothing);
  expect(env.server.registeredDevices, isEmpty);
  expect(env.server.pullCursors, isEmpty);
  expect(env.server.pushCalls, 0);
}

void main() {
  testWidgets(
    'Admin đăng nhập app → màn "chỉ dùng trên web", không đồng bộ; Đăng xuất '
    '→ màn đăng nhập, token đã xóa',
    (tester) async {
      final env = TestEnv(roles: const ['platform_admin'])..seed();
      await loginAndSetPin(tester, env);
      _expectBlocked(env, msgAdminWebOnly);

      await tester.tap(find.byKey(const ValueKey('blocked-logout')));
      await settle(tester, 30);
      expect(find.byType(LoginScreen), findsOneWidget);
      expect(await env.store.read('ag.oidc.access'), isNull);
      await finish(tester);
    },
  );

  testWidgets(
    'tài khoản không có vai nào của App HTX → báo không dùng được App HTX, '
    'không hiện màn nghiệp vụ nào',
    (tester) async {
      final env = TestEnv(roles: const ['kn_officer'])..seed();
      await loginAndSetPin(tester, env);
      _expectBlocked(env, msgNoHtxRole);
      expect(find.byKey(const ValueKey('blocked-logout')), findsOneWidget);
      await finish(tester);
    },
  );

  testWidgets('Admin kiêm Đại diện HTX → vẫn là màn chặn Admin', (
    tester,
  ) async {
    final env = TestEnv(roles: const ['platform_admin', 'coop_manager'])
      ..seed();
    await loginAndSetPin(tester, env);
    _expectBlocked(env, msgAdminWebOnly);
    await finish(tester);
  });
}
