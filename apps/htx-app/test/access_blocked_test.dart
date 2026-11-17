import 'dart:io';

import 'package:agrigreen_htx/ui/providers.dart';
import 'package:agrigreen_htx/ui/screens/access_blocked_screen.dart';
import 'package:agrigreen_htx/ui/screens/data_consent_screen.dart';
import 'package:agrigreen_htx/ui/screens/home_screen.dart';
import 'package:agrigreen_htx/ui/screens/households_screen.dart';
import 'package:agrigreen_htx/ui/screens/login_screen.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:path/path.dart' as p;

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

  testWidgets(
    'bản đệm /v1/me cũ có roles: [] nhưng access token mới có htx_member '
    '→ đăng nhập lại vào trang chủ, không kẹt ở màn chặn',
    (tester) async {
      // CSDL trên tệp: bản đệm `htx.me` còn lại sau khi đăng xuất.
      final dir = Directory.systemTemp.createTempSync('htx_me_');
      addTearDown(() {
        try {
          dir.deleteSync(recursive: true);
        } on FileSystemException {
          // Test trượt giữa chừng: CSDL còn mở, để lại thư mục tạm.
        }
      });
      final env = TestEnv(
        roles: const ['htx_member'],
        opener: MemoryDbOpener(file: p.join(dir.path, 'app.db')),
      )..seed();
      final me = env.server.onGet!;
      var revoked = true;
      env.server.onGet = (path, query) async => path == '/v1/me' && revoked
          ? {'roles': <String>[], 'org': null}
          : me(path, query);

      // CSDL trên tệp chạy I/O thật: cho các Future đó xong ngoài fake async.
      Future<void> pumpIo() async {
        for (var i = 0; i < 5; i++) {
          await tester.runAsync(
            () => Future<void>.delayed(const Duration(milliseconds: 50)),
          );
          await settle(tester);
        }
      }

      // Vai bị gỡ: token cũ còn htx_member nên vào app, lượt đồng bộ đầu ghi
      // `roles: []` vào bản đệm → màn chặn.
      await loginAndSetPin(tester, env);
      await pumpIo();
      final c = ProviderScope.containerOf(
        tester.element(find.byType(AccessBlockedScreen)),
      );
      expect(find.text(msgNoHtxRole), findsOneWidget);
      final cached = await tester.runAsync(
        () => c.read(appControllerProvider).services!.profile.roles(),
      );
      expect(cached, isEmpty);

      // Vai được cấp lại: đăng xuất, đăng nhập lại với token có htx_member;
      // bản đệm vẫn còn `roles: []`.
      revoked = false;
      await tester.tap(find.byKey(const ValueKey('blocked-logout')));
      await pumpIo();
      expect(find.byType(LoginScreen), findsOneWidget);
      await tester.tap(find.text('Đăng nhập'));
      await pumpIo();

      expect(find.byType(AccessBlockedScreen), findsNothing);
      expect(find.byType(HomeScreen), findsOneWidget);
      expect(find.text('Mở vụ hàng loạt'), findsOneWidget);

      // Đóng CSDL trước khi xóa thư mục tạm.
      await tester.runAsync(
        () => c.read(appControllerProvider.notifier).lock(),
      );
      await finish(tester);
    },
  );

  testWidgets(
    'bản đệm /v1/me cũ là htx_member nhưng access token mới có platform_admin '
    '→ màn chặn Admin ngay, không hỏi đồng ý, không đồng bộ',
    (tester) async {
      // CSDL trên tệp: bản đệm `htx.me` còn lại sau khi đăng xuất.
      final dir = Directory.systemTemp.createTempSync('htx_me_');
      addTearDown(() {
        try {
          dir.deleteSync(recursive: true);
        } on FileSystemException {
          // Test trượt giữa chừng: CSDL còn mở, để lại thư mục tạm.
        }
      });
      final env = TestEnv(
        roles: const ['htx_member'],
        opener: MemoryDbOpener(file: p.join(dir.path, 'app.db')),
      )..seed();

      Future<void> pumpIo() async {
        for (var i = 0; i < 5; i++) {
          await tester.runAsync(
            () => Future<void>.delayed(const Duration(milliseconds: 50)),
          );
          await settle(tester);
        }
      }

      // Thành viên HTX vào app; lượt đồng bộ đầu ghi bản đệm htx_member.
      await loginAndSetPin(tester, env);
      await pumpIo();
      expect(find.byType(HomeScreen), findsOneWidget);
      final c = ProviderScope.containerOf(
        tester.element(find.byType(HomeScreen)),
      );
      final cached = await tester.runAsync(
        () => c.read(appControllerProvider).services!.profile.roles(),
      );
      expect(cached, contains('htx_member'));

      // Tài khoản được cấp vai Admin: đăng xuất, đăng nhập lại với token mới.
      await tester.runAsync(
        () => c.read(appControllerProvider.notifier).signOut(),
      );
      await pumpIo();
      expect(find.byType(LoginScreen), findsOneWidget);
      env.oidc.roles = const ['platform_admin'];
      final devices = env.server.registeredDevices.length;
      final pulls = env.server.pullCursors.length;
      final pushes = env.server.pushCalls;
      await tester.tap(find.text('Đăng nhập'));
      await pumpIo();

      expect(find.byType(AccessBlockedScreen), findsOneWidget);
      expect(find.text(msgAdminWebOnly), findsOneWidget);
      expect(find.byType(HomeScreen), findsNothing);
      expect(find.byType(DataConsentScreen), findsNothing);
      expect(env.server.registeredDevices.length, devices);
      expect(env.server.pullCursors.length, pulls);
      expect(env.server.pushCalls, pushes);

      // Đóng CSDL trước khi xóa thư mục tạm.
      await tester.runAsync(
        () => c.read(appControllerProvider.notifier).lock(),
      );
      await finish(tester);
    },
  );

  testWidgets(
    'vai bị gỡ giữa phiên khi đang ở màn Nông hộ → lượt đồng bộ đóng màn đó, '
    'chỉ còn màn chặn',
    (tester) async {
      final env = TestEnv(roles: const ['htx_member'])..seed();
      final s = await loginToHome(tester, env);
      await openTile(tester, 'Nông hộ');
      expect(find.byType(HouseholdsScreen), findsOneWidget);

      final me = env.server.onGet!;
      env.server.onGet = (path, query) async => path == '/v1/me'
          ? {'roles': <String>[], 'org': null}
          : me(path, query);
      await s.sync.syncOnce(force: true);
      await settle(tester, 30);

      expect(find.byType(HouseholdsScreen, skipOffstage: false), findsNothing);
      expect(find.byType(HomeScreen, skipOffstage: false), findsNothing);
      expect(find.byType(AccessBlockedScreen), findsOneWidget);
      expect(find.text(msgNoHtxRole), findsOneWidget);
      expect(find.byType(BigTile, skipOffstage: false), findsNothing);
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
