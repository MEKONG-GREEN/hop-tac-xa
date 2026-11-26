import 'dart:async';

import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/test_env.dart';

TestEnv householdEnv(String role) {
  final env = TestEnv(roles: [role])..seed();
  env.location.result = const LocationResult(GpsStatus.denied);
  final base = env.server.onGet!;
  final seasons = <Map<String, Object?>>[];
  env.server.onGet = (path, query) async {
    if (path == '/v1/cooperatives/$coopRef/households') {
      return {
        'items': [
          {
            'household_ref': householdRef,
            'label': 'Hộ thử nghiệm',
            'cultivated_area_ha': '1.00',
            'active_area_ha': '0',
            'remaining_area_ha': '1.00',
            'can_open_season': true,
            'open_season_blocked_reason': '',
          },
        ],
        'next_cursor': null,
      };
    }
    if (path == '/v1/cooperatives/$coopRef/households/$householdRef/seasons') {
      return {'items': seasons, 'next_cursor': null};
    }
    if (path == '/v1/masterdata/catalogs/crop') {
      return {
        'items': [
          {'code': 'LUA', 'name': 'Lúa', 'attrs': <String, Object?>{}},
        ],
      };
    }
    if (path == '/v1/masterdata/catalogs/rice_variety') {
      return {
        'items': [
          {
            'code': 'OM18',
            'name': 'OM18',
            'attrs': {'crop_code': 'LUA'},
          },
        ],
      };
    }
    if (path == '/v1/cropcycle/sops') {
      return {
        'items': [
          {
            'ref': templateRef,
            'crop': 'LUA',
            'name': 'Quy trình lúa',
            'scope': 'system',
            'published_version': 1,
            'anchor_name': 'Ngày xuống giống',
          },
        ],
      };
    }
    if (path == '/v1/cropcycle/variety-bindings') return {'items': <Object?>[]};
    if (path == '/v1/cropcycle/sops/$templateRef') {
      return {
        'versions': [
          {'id': 'active-version', 'version_no': 1, 'state': 'active'},
        ],
      };
    }
    return base(path, query);
  };
  env.server.onSend = (method, path, body) async {
    if (path.endsWith('/season-plan-preview')) {
      return {'as_of_date': '2026-11-18', 'stages': <Object?>[]};
    }
    if (path != '/v1/cooperatives/$coopRef/households/$householdRef/seasons') {
      return null;
    }
    final season = <String, Object?>{
      ...body!,
      'season_ref': 'new-season',
      'status': 'open',
      'gps_missing': true,
      'sop_snapshot': {
        'name': 'Quy trình lúa',
        'anchor_name': 'Ngày xuống giống',
        'version_no': 1,
        'steps': [
          {'name': 'Chăm sóc'},
        ],
      },
    };
    seasons.add(season);
    return {'season': season};
  };
  return env;
}

Future<void> choose(WidgetTester tester, String label, String choice) async {
  await tapOn(
    tester,
    find.widgetWithText(DropdownButtonFormField<String>, label),
  );
  await tapOn(tester, find.text(choice).last);
}

void main() {
  testWidgets('AH-71: khóa mốc khi chờ preview, quay lại sửa thì tính mới', (
    tester,
  ) async {
    final env = householdEnv('htx_member');
    final normalSend = env.server.onSend!;
    final pending = Completer<Map<String, Object?>>();
    var delayed = false;
    env.server.onSend = (method, path, body) async {
      if (path.endsWith('/season-plan-preview') && !delayed) {
        delayed = true;
        return pending.future;
      }
      return normalSend(method, path, body);
    };
    await loginToHome(tester, env);
    await openTile(tester, 'Nông hộ');
    await tapText(tester, 'Hộ thử nghiệm');
    await tapText(tester, 'Mở vụ mới');
    await choose(tester, 'Loại cây', 'Lúa');
    await choose(tester, 'Mùa vụ', 'Đông Xuân');
    await tapText(tester, 'Tiếp tục');
    await choose(tester, 'Giống', 'OM18');
    await tester.enterText(
      find.widgetWithText(TextField, 'Diện tích vụ (ha)'),
      '0.25',
    );
    await tapText(tester, 'Tiếp tục');
    final anchor = find.widgetWithText(TextField, 'Mốc gốc (dd/mm/yyyy)');
    await tester.enterText(anchor, '01/01/2030');
    await tapText(tester, 'Tiếp tục');
    expect(tester.widget<TextField>(anchor).enabled, isFalse);
    pending.complete({'as_of_date': '2026-11-18', 'stages': <Object?>[]});
    await tester.pumpAndSettle();
    expect(find.text('Kế hoạch dự kiến'), findsOneWidget);
    await tapText(tester, 'Quay lại');
    expect(tester.widget<TextField>(anchor).enabled, isTrue);
    await tester.enterText(anchor, '02/01/2030');
    await tapText(tester, 'Tiếp tục');
    expect(find.text('Kế hoạch dự kiến'), findsOneWidget);
    final previews = env.server.sendCalls
        .where((r) => r.$2.endsWith('/season-plan-preview'))
        .toList();
    expect(previews, hasLength(2));
    expect(previews.last.$4?['anchor_date'], '2030-01-02');
    await finish(tester);
  });
  testWidgets('AH-61 Đại diện chỉ xem vụ dù hộ còn diện tích', (tester) async {
    final env = householdEnv('coop_manager');
    await loginToHome(tester, env);
    await openTile(tester, 'Nông hộ');
    await tapText(tester, 'Hộ thử nghiệm');
    expect(find.text('Chưa có vụ theo hộ.'), findsOneWidget);
    expect(find.text('Mở vụ mới'), findsNothing);
    await finish(tester);
  });
  testWidgets(
    'AH-61 nhiều bước, mốc gốc tự chọn, từ chối GPS vẫn mở trực tuyến và không xếp hàng',
    (tester) async {
      final env = householdEnv('htx_member');
      final services = await loginToHome(tester, env);
      await openTile(tester, 'Nông hộ');
      await tapText(tester, 'Hộ thử nghiệm');
      await tapText(tester, 'Mở vụ mới');
      await choose(tester, 'Loại cây', 'Lúa');
      await choose(tester, 'Mùa vụ', 'Đông Xuân');
      await tapText(tester, 'Tiếp tục');
      await choose(tester, 'Giống', 'OM18');
      await tester.enterText(
        find.widgetWithText(TextField, 'Diện tích vụ (ha)'),
        '0.25',
      );
      await tapText(tester, 'Tiếp tục');
      await tester.enterText(
        find.widgetWithText(TextField, 'Mốc gốc (dd/mm/yyyy)'),
        '01/01/2030',
      );
      await tapText(tester, 'Tiếp tục');
      expect(find.text('Kế hoạch dự kiến'), findsOneWidget);
      await tapText(tester, 'Quay lại');
      await tester.enterText(
        find.widgetWithText(TextField, 'Mốc gốc (dd/mm/yyyy)'),
        '02/01/2030',
      );
      await tapText(tester, 'Tiếp tục');
      final previews = env.server.sendCalls
          .where((r) => r.$2.endsWith('/season-plan-preview'))
          .toList();
      expect(previews, hasLength(2));
      expect(previews.last.$4?['anchor_date'], '2030-01-02');
      expect(previews.last.$3, isNot(previews.first.$3));
      await tapText(tester, 'Tiếp tục');
      expect(find.text('Thiếu GPS — vẫn lưu được.'), findsOneWidget);
      await tapText(tester, 'Lưu vụ');
      final posted = env.server.sendCalls
          .where(
            (r) =>
                r.$2 ==
                '/v1/cooperatives/$coopRef/households/$householdRef/seasons',
          )
          .single;
      expect(posted.$4?['anchor_date'], '2030-01-02');
      expect(posted.$4?['area_ha'], '0.25');
      expect(posted.$4?['gps'], {'source': 'denied'});
      expect(
        await services.outbox.list(entity: PushEntity.seasonOpen),
        isEmpty,
      );
      expect(find.textContaining('Đang canh tác'), findsOneWidget);
      await finish(tester);
    },
  );

  // AH-87: sản lượng lúa, rơm, ngày thu hoạch dự kiến của vụ theo hộ.
  TestEnv estimateEnv(String role) {
    final env = householdEnv(role);
    final get = env.server.onGet!;
    final send = env.server.onSend!;
    env.server.onGet = (path, query) async {
      if (path ==
          '/v1/cooperatives/$coopRef/households/$householdRef/seasons') {
        return {
          'items': [
            {
              'season_ref': 'season-87',
              'household_ref': householdRef,
              'season_code': 'DX',
              'season_year': 2026,
              'crop_code': 'LUA',
              'area_ha': '1.25',
              'status': 'open',
              'expected_harvest_date': '2027-01-20',
              'anchor_date': '2026-06-01',
              'current_event_id': 'event-87',
              'yield_estimate': {
                'seq': 1,
                'is_rice': true,
                'area_ha': '1.25',
                'conventional_yield_t_per_ha': '6',
                'straw_pct': '35',
                'computed_rice_t': '7.50',
                'computed_straw_t': '2.63',
                'entered_rice_t': null,
                'entered_straw_t': null,
                'rice_t': '7.50',
                'straw_t': '2.63',
              },
            },
          ],
          'next_cursor': null,
        };
      }
      return get(path, query);
    };
    env.server.onSend = (method, path, body) async {
      if (path.endsWith('/seasons/season-87/harvest')) {
        return {
          'season': {
            'season_ref': 'season-87',
            'household_ref': householdRef,
            'season_code': 'DX',
            'season_year': 2026,
            'crop_code': 'LUA',
            'area_ha': '1.25',
            'status': 'completed',
            'expected_harvest_date': '2027-01-20',
            'harvest': {
              'mass_t': '8.40',
              'harvest_date': '2026-09-20',
              'yield_t_per_ha': '6.72',
            },
          },
          'harvest': {'mass_t': '8.40'},
          'auto_completed_steps': [5],
        };
      }
      if (path.endsWith('/seasons/season-87/yield-estimates')) {
        return {
          'yield_estimate': {
            'seq': 2,
            'is_rice': true,
            'area_ha': '1.25',
            'conventional_yield_t_per_ha': '6',
            'straw_pct': '35',
            'computed_rice_t': '7.50',
            'computed_straw_t': '3.50',
            'entered_rice_t': '10.00',
            'entered_straw_t': null,
            'rice_t': '10.00',
            'straw_t': '3.50',
          },
        };
      }
      return send(method, path, body);
    };
    return env;
  }

  testWidgets('AH-87 Thành viên xem và nhập lúa dự kiến, kiểm số tấn', (
    tester,
  ) async {
    final env = estimateEnv('htx_member');
    await loginToHome(tester, env);
    await openTile(tester, 'Nông hộ');
    await tapText(tester, 'Hộ thử nghiệm');
    expect(find.textContaining('Lúa dự kiến 7,50 tấn'), findsOneWidget);
    await tapOn(tester, find.byTooltip('Sản lượng'));
    expect(find.text('7,50 tấn'), findsOneWidget);
    expect(find.text('2,63 tấn'), findsOneWidget);
    expect(find.text('20/01/2027'), findsOneWidget);
    await tapText(tester, 'Sửa sản lượng dự kiến');
    final rice = find.widgetWithText(TextField, 'Lúa dự kiến (tấn)');
    await tester.enterText(rice, '-1');
    await tapText(tester, 'Lưu');
    expect(
      find.text('Nhập số tấn không âm, tối đa 2 chữ số thập phân.'),
      findsOneWidget,
    );
    expect(
      env.server.sendCalls.where((c) => c.$2.endsWith('/yield-estimates')),
      isEmpty,
    );
    await tester.enterText(rice, '10');
    await tapText(tester, 'Lưu');
    final call = env.server.sendCalls.singleWhere(
      (c) => c.$2.endsWith('/yield-estimates'),
    );
    expect(call.$4, {'expected_seq': 1, 'rice_t': '10', 'straw_t': null});
    expect(find.text('10,00 tấn'), findsOneWidget);
    expect(find.text('3,50 tấn'), findsOneWidget);
    await finish(tester);
  });

  testWidgets('AH-87 Đại diện chỉ xem sản lượng dự kiến', (tester) async {
    final env = estimateEnv('coop_manager');
    await loginToHome(tester, env);
    await openTile(tester, 'Nông hộ');
    await tapText(tester, 'Hộ thử nghiệm');
    await tapOn(tester, find.byTooltip('Sản lượng'));
    expect(find.text('7,50 tấn'), findsOneWidget);
    expect(find.text('Sửa sản lượng dự kiến'), findsNothing);
    await finish(tester);
  });

  testWidgets('AH-100 Thành viên khai báo sản lượng, 0 cần xác nhận', (
    tester,
  ) async {
    final env = estimateEnv('htx_member');
    await loginToHome(tester, env);
    await openTile(tester, 'Nông hộ');
    await tapText(tester, 'Hộ thử nghiệm');
    await tapOn(tester, find.byTooltip('Sản lượng'));
    await tapText(tester, 'Khai báo sản lượng');
    final mass = find.widgetWithText(TextField, 'Tổng sản lượng (tấn)');
    final day = find.widgetWithText(
      TextField,
      'Ngày thu hoạch cuối (dd/mm/yyyy)',
    );
    await tester.enterText(day, '20/09/2026');
    await tester.enterText(mass, '-1');
    await tapText(tester, 'Lưu khai báo');
    expect(
      find.text('Nhập số tấn không âm, tối đa 2 chữ số thập phân.'),
      findsOneWidget,
    );
    await tester.enterText(day, '31/05/2026');
    await tester.enterText(mass, '8,4');
    await tapText(tester, 'Lưu khai báo');
    expect(find.textContaining('từ mốc gốc'), findsOneWidget);
    await tester.enterText(day, '20/09/2026');
    await tester.enterText(mass, '0');
    await tapText(tester, 'Lưu khai báo');
    expect(find.text('Xác nhận lưu'), findsOneWidget);
    expect(
      env.server.sendCalls.where((c) => c.$2.endsWith('/harvest')),
      isEmpty,
    );
    await tester.enterText(mass, '8,4');
    await tapText(tester, 'Lưu khai báo');
    final call = env.server.sendCalls.singleWhere(
      (c) => c.$2.endsWith('/harvest'),
    );
    expect(call.$4?['mass_t'], '8.4');
    expect(call.$4?['harvest_date'], '2026-09-20');
    expect(call.$4?['zero_confirmed'], false);
    expect(call.$4?['expected_season_event_id'], 'event-87');
    expect(find.text('8,40 tấn'), findsOneWidget);
    expect(find.text('Khai báo sản lượng'), findsNothing);
    await finish(tester);
  });

  testWidgets('AH-100 Đại diện không có nút khai báo', (tester) async {
    final env = estimateEnv('coop_manager');
    await loginToHome(tester, env);
    await openTile(tester, 'Nông hộ');
    await tapText(tester, 'Hộ thử nghiệm');
    await tapOn(tester, find.byTooltip('Sản lượng'));
    expect(find.text('Khai báo sản lượng'), findsNothing);
    await finish(tester);
  });

  // AH-189: vụ "Đã thu hoạch" — Thành viên sửa sản lượng, mọi vai xem lịch sử.
  TestEnv harvestedEnv(String role) {
    final env = estimateEnv(role);
    final get = env.server.onGet!;
    final send = env.server.onSend!;
    Map<String, Object?> season(String mass, String day) => {
      'season_ref': 'season-87',
      'household_ref': householdRef,
      'season_code': 'DX',
      'season_year': 2026,
      'crop_code': 'LUA',
      'area_ha': '1.25',
      'status': 'completed',
      'anchor_date': '2026-06-01',
      'current_event_id': 'event-87',
      'harvest': {
        'harvest_id': 'h1',
        'mass_t': mass,
        'harvest_date': day,
        'yield_t_per_ha': '6.72',
      },
    };
    env.server.onGet = (path, query) async {
      if (path ==
          '/v1/cooperatives/$coopRef/households/$householdRef/seasons') {
        return {
          'items': [season('8.40', '2026-09-20')],
          'next_cursor': null,
        };
      }
      if (path.endsWith('/seasons/season-87/harvest/history')) {
        return {
          'items': [
            {
              'kind': 'declared',
              'mass_t': '8.40',
              'harvest_date': '2026-09-20',
              'reason': null,
            },
            {
              'kind': 'corrected',
              'mass_t': '9.10',
              'harvest_date': '2026-09-21',
              'reason': 'Cân lại tại kho HTX',
            },
          ],
        };
      }
      return get(path, query);
    };
    env.server.onSend = (method, path, body) async {
      if (path.endsWith('/seasons/season-87/harvest/corrections')) {
        return {
          'season': season('9.10', '2026-09-21'),
          'harvest': {'mass_t': '9.10'},
        };
      }
      return send(method, path, body);
    };
    return env;
  }

  testWidgets('AH-189 Thành viên sửa sản lượng, xem lịch sử', (tester) async {
    final env = harvestedEnv('htx_member');
    await loginToHome(tester, env);
    await openTile(tester, 'Nông hộ');
    await tapText(tester, 'Hộ thử nghiệm');
    expect(find.textContaining('Đã thu hoạch'), findsOneWidget);
    await tapOn(tester, find.byTooltip('Sản lượng'));
    expect(find.text('8,40 tấn'), findsOneWidget);
    expect(find.text('Khai báo sản lượng'), findsNothing);
    await tapText(tester, 'Sửa sản lượng');
    final mass = find.widgetWithText(TextField, 'Tổng sản lượng (tấn)');
    final reason = find.widgetWithText(TextField, 'Lý do sửa (không bắt buộc)');
    await tester.enterText(mass, '9,1');
    await tester.enterText(
      find.widgetWithText(TextField, 'Ngày thu hoạch cuối (dd/mm/yyyy)'),
      '21/09/2026',
    );
    await tester.enterText(reason, 'ngắn');
    await tapText(tester, 'Lưu sửa');
    expect(find.textContaining('Lý do sửa từ 10'), findsOneWidget);
    await tester.enterText(reason, 'Cân lại tại kho HTX');
    await tapText(tester, 'Lưu sửa');
    final call = env.server.sendCalls.singleWhere(
      (c) => c.$2.endsWith('/harvest/corrections'),
    );
    expect(call.$4, {
      'expected_harvest_id': 'h1',
      'mass_t': '9.1',
      'harvest_date': '2026-09-21',
      'zero_confirmed': false,
      'reason': 'Cân lại tại kho HTX',
      'channel': 'app',
    });
    expect(find.text('9,10 tấn'), findsOneWidget);
    await tapText(tester, 'Xem lịch sử khai báo');
    expect(find.textContaining('Sửa: 9,10 tấn'), findsOneWidget);
    expect(find.text('Lý do: Cân lại tại kho HTX'), findsOneWidget);
    expect(find.text('Hủy khai báo'), findsNothing);
    await finish(tester);
  });

  testWidgets('AH-189 Đại diện không có nút sửa, vẫn xem lịch sử', (
    tester,
  ) async {
    final env = harvestedEnv('coop_manager');
    await loginToHome(tester, env);
    await openTile(tester, 'Nông hộ');
    await tapText(tester, 'Hộ thử nghiệm');
    await tapOn(tester, find.byTooltip('Sản lượng'));
    expect(find.text('Sửa sản lượng'), findsNothing);
    await tapText(tester, 'Xem lịch sử khai báo');
    expect(find.textContaining('Khai báo: 8,40 tấn'), findsOneWidget);
    await finish(tester);
  });

  // AH-152: "Ghi lại vụ" — tổng kết, hoàn thành vụ (xác nhận hai bước), Đại diện chỉ xem.
  TestEnv summaryEnv(String role, {String status = 'completed'}) {
    final env = harvestedEnv(role);
    final get = env.server.onGet!;
    final send = env.server.onSend!;
    var current = status;
    Map<String, Object?> summary(String st) => {
      'status': st,
      'area_ha': '1.25',
      'mass_t': status == 'open' ? null : '8.40',
      'yield_t_per_ha': status == 'open' ? null : '6.72',
      'harvest_date': status == 'open' ? null : '2026-09-20',
      'materials': [
        {'name': 'URE', 'unit': 'kg', 'qty': '62.5'},
      ],
      'tasks': {
        'early': 1,
        'on_time': 2,
        'late': 1,
        'skipped': 1,
        'not_done': 0,
        'total': 5,
      },
      'summary_step_seq': 6,
      'can_close': st == 'completed',
      'blockers': st == 'completed' ? <String>[] : ['harvest_required'],
      'unresolved_steps': <int>[],
      'harvest_reminder': st == 'open',
      'cost_vnd': st == 'closed' ? '15000000' : null,
      'note': st == 'closed' ? 'Vụ được mùa' : null,
      'closed_at': st == 'closed' ? '2026-09-29T03:00:00Z' : null,
      'closed_by': null,
    };
    env.server.onGet = (path, query) async {
      if (path.endsWith('/seasons/season-87/summary')) return summary(current);
      if (status == 'open' &&
          path ==
              '/v1/cooperatives/$coopRef/households/$householdRef/seasons') {
        final r = (await get(path, query))!;
        final items = (r['items'] as List)
            .map((e) => {...(e as Map), 'status': 'open', 'harvest': null})
            .toList();
        return {...r, 'items': items};
      }
      return get(path, query);
    };
    env.server.onSend = (method, path, body) async {
      if (path.endsWith('/seasons/season-87/close')) {
        current = 'closed';
        return {
          'season': {
            'season_ref': 'season-87',
            'household_ref': householdRef,
            'status': 'closed',
            'anchor_date': '2026-06-01',
            'current_event_id': 'event-88',
            'area_ha': '1.25',
            'harvest': {
              'harvest_id': 'h1',
              'mass_t': '8.40',
              'harvest_date': '2026-09-20',
              'yield_t_per_ha': '6.72',
            },
          },
          'summary': summary('closed'),
        };
      }
      return send(method, path, body);
    };
    return env;
  }

  testWidgets('AH-152 Thành viên xem tổng kết và hoàn thành vụ', (
    tester,
  ) async {
    final env = summaryEnv('htx_member');
    await loginToHome(tester, env);
    await openTile(tester, 'Nông hộ');
    await tapText(tester, 'Hộ thử nghiệm');
    await tapOn(tester, find.byTooltip('Sản lượng'));
    expect(find.text('Ghi lại vụ'), findsOneWidget);
    expect(find.text('6,72 tấn/ha'), findsOneWidget);
    expect(find.text('URE: 62,5 kg'), findsOneWidget);
    expect(
      find.text(
        'Sớm 1 · Đúng hạn 2 · Trễ 1 · Bỏ qua 1 · Chưa làm 0 (tổng 5 việc)',
      ),
      findsOneWidget,
    );
    await tapText(tester, 'Hoàn thành vụ');
    final cost = find.widgetWithText(
      TextField,
      'Chi phí cả vụ (đồng, không bắt buộc)',
    );
    final note = find.widgetWithText(TextField, 'Ghi chú (không bắt buộc)');
    await tester.enterText(cost, '15.000.000');
    await tester.enterText(note, 'Gọi 0912345678');
    await tapText(tester, 'Lưu hoàn thành vụ');
    expect(find.text('Ghi chú không được chứa SĐT hoặc CCCD.'), findsOneWidget);
    await tester.enterText(note, 'Vụ được mùa');
    await tapText(tester, 'Lưu hoàn thành vụ');
    expect(find.textContaining('không quay lại'), findsOneWidget);
    expect(env.server.sendCalls.where((c) => c.$2.endsWith('/close')), isEmpty);
    await tapText(tester, 'Xác nhận hoàn thành vụ');
    final call = env.server.sendCalls.singleWhere(
      (c) => c.$2.endsWith('/close'),
    );
    expect(call.$4, {
      'expected_season_event_id': 'event-87',
      'cost_vnd': '15000000',
      'note': 'Vụ được mùa',
      'gps': {'source': 'denied'},
      'channel': 'app',
    });
    expect(find.text('Ghi lại vụ · Đã hoàn thành vụ'), findsOneWidget);
    expect(find.text('15.000.000 đ'), findsOneWidget);
    expect(find.text('Hoàn thành vụ'), findsOneWidget);
    await finish(tester);
  });

  testWidgets('AH-152 Đại diện chỉ xem tổng kết, không có nút hoàn thành', (
    tester,
  ) async {
    final env = summaryEnv('coop_manager');
    await loginToHome(tester, env);
    await openTile(tester, 'Nông hộ');
    await tapText(tester, 'Hộ thử nghiệm');
    await tapOn(tester, find.byTooltip('Sản lượng'));
    expect(find.text('Ghi lại vụ'), findsOneWidget);
    expect(find.widgetWithText(FilledButton, 'Hoàn thành vụ'), findsNothing);
    await finish(tester);
  });

  testWidgets('AH-152 nhắc khai báo sản lượng khi đã xong giai đoạn cuối', (
    tester,
  ) async {
    final env = summaryEnv('htx_member', status: 'open');
    await loginToHome(tester, env);
    await openTile(tester, 'Nông hộ');
    await tapText(tester, 'Hộ thử nghiệm');
    await tapOn(tester, find.byTooltip('Sản lượng'));
    expect(find.byKey(const ValueKey('harvest-reminder')), findsOneWidget);
    expect(find.widgetWithText(FilledButton, 'Hoàn thành vụ'), findsNothing);
    await finish(tester);
  });
}
