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
      expect(find.text('Thiếu GPS — vẫn lưu được.'), findsOneWidget);
      await tapText(tester, 'Lưu vụ');
      final posted = env.server.sendCalls
          .where(
            (r) =>
                r.$2 ==
                '/v1/cooperatives/$coopRef/households/$householdRef/seasons',
          )
          .single;
      expect(posted.$4?['anchor_date'], '2030-01-01');
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
}
