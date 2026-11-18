import 'package:agrigreen_mobile/data/actions/action_queue.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/test_env.dart';

const logA = '01920000-0000-7000-8000-00000000f0a1';
const logB = '01920000-0000-7000-8000-00000000f0b1';
const logC = '01920000-0000-7000-8000-00000000f0c1';

Map<String, Object?> _pendingLog(String id, String status, String gps) => {
  'entity': 'farm_log',
  'id': id,
  'op': 'upsert',
  'data': {
    'id': id,
    'log_ref': id,
    'season_ref': '01920000-0000-7000-8000-00000000e001',
    'plot_ref': plotRef,
    'activity_code': 'phun_thuoc',
    'event_time': '2026-09-30T07:00:00+07:00',
    'gps_source': gps,
    'location_status': status,
    'machine_codes': [],
    'inputs': [],
    'note': 'Phun ${id.substring(34)}',
    'file_refs': [],
    'on_behalf': false,
    'status': 'pending',
  },
};

void main() {
  testWidgets(
    'AH-50: Thành viên xem hộ hết diện tích, không mở thửa chưa có vụ',
    (tester) async {
      final env = TestEnv(roles: const ['htx_member'])..seed();
      final base = env.server.onGet!;
      env.server.onGet = (path, q) async {
        if (path == '/v1/me') {
          return {
            'roles': ['htx_member'],
            'org': {'ref': coopRef, 'name': 'HTX Tân Phú'},
          };
        }
        if (path == '/v1/cooperatives/$coopRef/households') {
          return {
            'items': [
              {
                'household_ref': householdRef,
                'label': 'Hộ hết diện tích',
                'cultivated_area_ha': '2.50',
                'active_area_ha': '2.50',
                'remaining_area_ha': '0',
                'can_open_season': false,
                'open_season_blocked_reason': 'Hết diện tích để mở vụ.',
              },
            ],
            'next_cursor': null,
          };
        }
        if (path ==
            '/v1/cooperatives/$coopRef/households/$householdRef/seasons') {
          return {'items': <Object?>[], 'next_cursor': null};
        }
        return base(path, q);
      };
      await loginToHome(tester, env);
      await openTile(tester, 'Nông hộ');
      expect(find.text('Hộ hết diện tích'), findsOneWidget);
      expect(find.textContaining('Còn lại: 0,00 ha'), findsOneWidget);
      await tapText(tester, 'Hộ hết diện tích');
      expect(find.text('Mở vụ mới'), findsNothing);
      await tapText(tester, 'Xem thửa và vụ theo thửa đang có');
      final tile = tester.widget<ListTile>(
        find.widgetWithText(ListTile, 'T1 · Ruộng bờ kênh'),
      );
      expect(tile.onTap, isNull);
      expect(find.text('Mở vụ mới'), findsNothing);
      await finish(tester);
    },
  );

  testWidgets(
    'AH-50: cache ngoại tuyến không cấp khả năng mở vụ; lỗi quyền không trả cache',
    (tester) async {
      final env = TestEnv(roles: const ['coop_manager'])..seed();
      final base = env.server.onGet!;
      var offline = false;
      var denied = false;
      env.server.onGet = (path, q) async {
        if (path == '/v1/cooperatives/$coopRef/households') {
          if (denied) throw ApiException(403, 'forbidden');
          if (offline) throw NetworkException('offline');
          return {
            'items': [
              {
                'household_ref': householdRef,
                'label': 'Hộ còn đất',
                'cultivated_area_ha': '2.50',
                'active_area_ha': '0',
                'remaining_area_ha': '2.50',
                'can_open_season': true,
                'open_season_blocked_reason': '',
              },
            ],
            'next_cursor': null,
          };
        }
        return base(path, q);
      };
      final services = await loginToHome(tester, env);
      expect(
        (await services.manager.households()).single.canOpenSeason,
        isTrue,
      );
      offline = true;
      final cached = (await services.manager.households()).single;
      expect(cached.remainingAreaHa, '2.50');
      expect(cached.areaFresh, isFalse);
      expect(cached.canOpenSeason, isFalse);
      offline = false;
      denied = true;
      await expectLater(
        services.manager.households(),
        throwsA(isA<ApiException>()),
      );
      await expectLater(
        services.manager.households(cooperativeRef: 'other-coop'),
        throwsA(isA<Exception>()),
      );
      await finish(tester);
    },
  );

  testWidgets(
    'HTX US-LOG-05: nhãn vị trí rõ khi duyệt; phê duyệt / yêu cầu bổ sung xếp '
    'hàng rồi gửi POST /reviews; tự duyệt bị máy chủ chặn → báo rõ',
    (tester) async {
      final env = TestEnv(roles: const ['coop_manager'])
        ..seed(
          season: true,
          extra: [
            _pendingLog(logA, 'mismatch', 'device'),
            _pendingLog(logB, 'missing', 'denied'),
            _pendingLog(logC, 'match', 'device'),
          ],
        );
      env.server.actionErrors['/v1/cropcycle/farm-logs/$logC/reviews'] = (
        403,
        'cropcycle.self_review',
      );
      final s = await loginToHome(tester, env);
      await openTile(tester, 'Duyệt nhật ký');
      expect(find.text('Vị trí không khớp'), findsOneWidget);
      expect(find.text('Thiếu GPS'), findsOneWidget);

      await tapOn(tester, find.byKey(const ValueKey('approve-$logA')));
      await tapOn(tester, find.byKey(const ValueKey('supplement-$logB')));
      await tester.enterText(
        find.byKey(const ValueKey('review-note')),
        'Chụp thêm ảnh ruộng',
      );
      await tapText(tester, 'Gửi yêu cầu');
      await tapOn(tester, find.byKey(const ValueKey('approve-$logC')));
      await s.sync.syncOnce(force: true);

      final paths = env.server.actionCalls.map((c) => c.$1).toList();
      expect(
        paths,
        containsAll([
          '/v1/cropcycle/farm-logs/$logA/reviews',
          '/v1/cropcycle/farm-logs/$logB/reviews',
        ]),
      );
      final calls = await s.actions.ofKind(ApiActionKind.farmLogReview);
      final b = calls.firstWhere((a) => a.target == logB);
      expect(b.body, {
        'decision': 'needs_supplement',
        'note': 'Chụp thêm ảnh ruộng',
      });
      expect(calls.firstWhere((a) => a.target == logA).body, {
        'decision': 'approved',
      });

      await tester.pageBack();
      await settle(tester);
      await openTile(tester, 'Duyệt nhật ký');
      expect(
        find.text('Người ghi nhật ký không được tự duyệt.'),
        findsOneWidget,
      );
      await finish(tester);
    },
  );

  testWidgets(
    'HTX US-DASH-01: dashboard đọc API, đổi bộ lọc vụ cập nhật; mất mạng → bản gần nhất',
    (tester) async {
      final env = TestEnv(roles: const ['coop_manager'])..seed();
      final base = env.server.onGet!;
      final dashQueries = <Map<String, String>?>[];
      var offline = false;
      env.server.onGet = (path, q) async {
        if (path == '/v1/cropcycle/dashboard') {
          if (offline) throw NetworkException('offline');
          dashQueries.add(q);
          return {
            'totals': {
              'area_ha': '120.5',
              'households': 42,
              'expected_kg': '723000',
              'actual_kg': '650500.5',
              'actual_yield_t_per_ha': '6.2',
              'sop_progress_pct': '64.5',
              'approved_logs': 310,
              'pending_logs': 12,
            },
            'by_variety': [
              {
                'key': 'OM18',
                'expected_kg': '400000',
                'actual_kg': '380000',
                'area_ha': '70',
              },
            ],
            'by_season': [],
            'by_cooperative': [],
          };
        }
        return base(path, q);
      };
      await loginToHome(tester, env);
      await openTile(tester, 'Tổng quan');
      expect(find.text('120,5 ha'), findsOneWidget);
      expect(find.text('723,0 tấn'), findsOneWidget);
      expect(find.text('650,5 tấn'), findsOneWidget);
      expect(find.text('64,5 %'), findsOneWidget);
      await tapText(tester, 'Thu Đông');
      expect(dashQueries.last, {'season_code': 'TD', 'season_year': '2026'});

      offline = true;
      await tapText(tester, 'Mọi vụ');
      await tapText(tester, 'Thu Đông');
      expect(find.textContaining('Mất mạng — số liệu lúc'), findsOneWidget);
      expect(find.text('120,5 ha'), findsOneWidget);
      await finish(tester);
    },
  );

  testWidgets('AH-61: Thành viên HTX chọn nông hộ rồi mở vụ theo hộ', (
    tester,
  ) async {
    final env = TestEnv(roles: const ['htx_member'])..seed();
    final base = env.server.onGet!;
    env.server.onGet = (path, q) async =>
        path == '/v1/cooperatives/$coopRef/households'
        ? {
            'items': [
              {
                'ref': householdRef,
                'label': 'Nguyễn V. A.',
                'code': 'HH-01',
                'cultivated_area_ha': '2.50',
                'active_area_ha': '0.50',
                'remaining_area_ha': '2.00',
                'can_open_season': true,
                'open_season_blocked_reason': '',
              },
            ],
            'next_cursor': null,
          }
        : path == '/v1/cooperatives/$coopRef/households/$householdRef/seasons'
        ? {'items': <Object?>[], 'next_cursor': null}
        : path == '/v1/masterdata/catalogs/crop'
        ? {'items': <Object?>[], 'next_cursor': null}
        : base(path, q);
    await loginToHome(tester, env);
    await openTile(tester, 'Nông hộ');
    expect(find.text('Nguyễn V. A.'), findsOneWidget);
    expect(find.textContaining('2 lô ruộng'), findsOneWidget);
    expect(
      find.textContaining(
        'Đất: 2,50 ha · Đang canh tác: 0,50 ha · Còn lại: 2,00 ha',
      ),
      findsOneWidget,
    );
    await tapText(tester, 'Nguyễn V. A.');
    expect(find.text('Mở vụ mới'), findsOneWidget);
    await tapText(tester, 'Mở vụ mới');
    expect(find.text('Khai báo vụ mới'), findsOneWidget);
    expect(find.text('Loại cây và mùa vụ'), findsOneWidget);
    await finish(tester);
  });
}
