import 'package:agrigreen_htx/ui/providers.dart';
import 'package:agrigreen_htx/ui/screens/data_consent_screen.dart';
import 'package:agrigreen_htx/ui/screens/home_screen.dart';
import 'package:agrigreen_htx/ui/screens/login_screen.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/data/sync/sync_service.dart';
import 'package:agrigreen_mobile/testing.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/test_env.dart';

/// Đăng nhập (OIDC giả) → tạo PIN, dừng trước khi vào trang chủ.
Future<void> loginAndSetPin(WidgetTester tester, TestEnv env) async {
  await tester.pumpWidget(appUnderTest(env));
  await settle(tester);
  await tester.tap(find.text('Đăng nhập'));
  await settle(tester);
  for (var round = 0; round < 2; round++) {
    for (final d in ['1', '3', '5', '7', '9', '2']) {
      await tester.tap(find.text(d));
      await tester.pump();
    }
    await settle(tester, 10);
  }
  await settle(tester, 30);
}

void main() {
  // AH-23 / REQ-011 / KH-24: Đồng ý thu thập ảnh GPS và dữ liệu sản xuất.
  testWidgets('AC1/AC2: tài khoản HTX chưa đồng ý → sau PIN thấy màn Đồng ý kèm nội dung; '
      'bấm Đồng ý → trang chủ, máy chủ nhận đúng phiên bản', (tester) async {
    final env = TestEnv(roles: const ['coop_manager'])..seed();
    env.server.dataConsentRequired = true;
    await loginAndSetPin(tester, env);
    expect(find.byType(DataConsentScreen), findsOneWidget);
    expect(find.byType(HomeScreen), findsNothing);
    expect(find.text('Đồng ý thu thập dữ liệu'), findsOneWidget);
    expect(find.text(env.server.dataConsentText), findsOneWidget);
    expect(find.text('Phiên bản nội dung: tam-1'), findsOneWidget);
    // Chưa đồng ý: chưa đồng bộ, chưa gửi gì.
    expect(env.server.pushCalls, 0);

    await tester.tap(find.byKey(const ValueKey('consent-accept')));
    await settle(tester, 30);
    expect(find.byType(HomeScreen), findsOneWidget);
    expect(env.server.consentPosts.single['version'], 'tam-1');
    expect(env.server.dataConsentRequired, isFalse);
    await finish(tester);
  });

  testWidgets(
    'AC-N3: bấm Không đồng ý → về màn đăng nhập, không ghi đồng ý, phiên ngoại '
    'tuyến bị xóa',
    (tester) async {
      final env = TestEnv(roles: const ['coop_manager'])..seed();
      env.server.dataConsentRequired = true;
      await loginAndSetPin(tester, env);
      await tester.tap(find.byKey(const ValueKey('consent-decline')));
      await settle(tester, 30);
      expect(find.byType(LoginScreen), findsOneWidget);
      expect(find.byType(DataConsentScreen), findsNothing);
      expect(env.server.consentPosts, isEmpty);
      expect(await env.store.read('ag.oidc.access'), isNull);
      expect(await env.store.read('htx.data_consent'), isNull);
      await finish(tester);
    },
  );

  testWidgets('AC-N1: đã đồng ý phiên bản hiện hành → vào thẳng trang chủ', (
    tester,
  ) async {
    final env = TestEnv(roles: const ['coop_manager'])..seed();
    await loginToHome(tester, env);
    expect(find.byType(DataConsentScreen), findsNothing);
    expect(env.server.consentPosts, isEmpty);
    // Bản nhớ theo tài khoản (dùng được ngoại tuyến).
    expect(await env.store.read('htx.data_consent'), 'u-1|tam-1');
    await finish(tester);
  });

  testWidgets(
    'nội dung vừa đổi (409) → màn Đồng ý đọc lại bản mới kèm thông báo',
    (tester) async {
      final env = TestEnv(roles: const ['coop_manager'])..seed();
      env.server.dataConsentRequired = true;
      await loginAndSetPin(tester, env);
      env.server.dataConsentVersion = 'tam-2';
      env.server.dataConsentText = 'Nội dung mới';
      await tester.tap(find.byKey(const ValueKey('consent-accept')));
      await settle(tester, 30);
      expect(find.byType(DataConsentScreen), findsOneWidget);
      expect(find.text('Phiên bản nội dung: tam-2'), findsOneWidget);
      expect(find.text('Nội dung mới'), findsOneWidget);
      expect(find.byKey(const ValueKey('consent-message')), findsOneWidget);
      await tester.tap(find.byKey(const ValueKey('consent-accept')));
      await settle(tester, 30);
      expect(find.byType(HomeScreen), findsOneWidget);
      expect(env.server.consentPosts.map((p) => p['version']), [
        'tam-1',
        'tam-2',
      ]);
      await finish(tester);
    },
  );

  testWidgets(
    'AC3 phía app: ảnh chờ tải bị máy chủ từ chối vì chưa đồng ý → ảnh vẫn '
    'chờ (không đánh dấu hỏng), app mở lại màn Đồng ý',
    (tester) async {
      final env = TestEnv(roles: const ['coop_manager'])..seed();
      final s = await loginToHome(tester, env);
      final shot = await s.photos.capture(
        subjectRef: householdRef,
        parentEntity: 'farm_log',
      );
      expect(shot, isNotNull);
      expect(await s.evidence.pending(), hasLength(1));
      env.server.dataConsentRequired = true;
      await s.sync.syncOnce(force: true);
      await settle(tester, 30);
      final pending = await s.evidence.pending();
      expect(pending, hasLength(1));
      expect(pending.single.status, isNot(contains('fail')));
      expect(
        (await s.sync.status()).lastError,
        DataConsentRequiredException.consentCode,
      );
      expect((await s.sync.status()).state, SyncState.error);
      expect(env.server.files, isEmpty);
      expect(find.byType(DataConsentScreen), findsOneWidget);
      expect(
        containerOf(tester).read(appControllerProvider).consent?.isPrompt,
        isTrue,
      );

      // Đồng ý lại → ảnh được tải lên ở lượt đồng bộ kế tiếp.
      await tester.tap(find.byKey(const ValueKey('consent-accept')));
      await settle(tester, 30);
      expect(find.byType(HomeScreen), findsOneWidget);
      await s.sync.syncOnce(force: true);
      expect(env.server.files, hasLength(1));
      await finish(tester);
    },
  );

  testWidgets(
    'không xác nhận được (mất mạng, chưa có bản nhớ) → màn chặn có Thử lại và '
    'Đăng xuất, không có nút Đồng ý',
    (tester) async {
      final server = _OfflineConsentServer();
      final env = TestEnv(roles: const ['coop_manager'], server: server)
        ..seed();
      await loginAndSetPin(tester, env);
      expect(find.byType(DataConsentScreen), findsOneWidget);
      expect(find.byType(HomeScreen), findsNothing);
      expect(find.byKey(const ValueKey('consent-accept')), findsNothing);
      expect(
        find.text('Cần kết nối mạng để xác nhận Đồng ý thu thập dữ liệu.'),
        findsOneWidget,
      );
      expect(server.pushCalls, 0);

      // Có mạng trở lại → Thử lại → vào được.
      server.offline = false;
      await tester.tap(find.byKey(const ValueKey('consent-retry')));
      await settle(tester, 30);
      expect(find.byType(HomeScreen), findsOneWidget);
      await finish(tester);
    },
  );

  testWidgets('chưa xác nhận được → Đăng xuất về màn đăng nhập', (
    tester,
  ) async {
    final env = TestEnv(
      roles: const ['coop_manager'],
      server: _OfflineConsentServer(),
    )..seed();
    await loginAndSetPin(tester, env);
    await tester.tap(find.byKey(const ValueKey('consent-logout')));
    await settle(tester, 30);
    expect(find.byType(LoginScreen), findsOneWidget);
    await finish(tester);
  });

  testWidgets('nông dân (farmer) không thấy màn Đồng ý', (tester) async {
    final env = TestEnv()..seed();
    await loginToHome(tester, env);
    expect(find.byType(DataConsentScreen), findsNothing);
    expect(env.server.consentPosts, isEmpty);
    await finish(tester);
  });
}

/// Máy chủ mất mạng đúng lúc hỏi trạng thái đồng ý (tắt bằng [offline]).
class _OfflineConsentServer extends FakeServer {
  bool offline = true;

  @override
  Future<Map<String, Object?>> getJson(
    String path, {
    Map<String, String>? query,
  }) {
    if (offline && path == '/v1/me/data-consent') {
      throw NetworkException('offline');
    }
    return super.getJson(path, query: query);
  }
}
