import 'package:agrigreen_htx/application/household_season_service.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'household_season_screen_test.dart' show householdEnv, choose;
import 'support/test_env.dart';

const calendarRef = '01920000-0000-7000-8000-00000000ca11';
Map<String, Object?> calendar() => {
  'calendar_ref': calendarRef,
  'cooperative_ref': coopRef,
  'current_event_id': '01920000-0000-7000-8000-00000000ca12',
  'version': 1,
  'status': 'published',
  'crop_code': 'LUA',
  'season_code': 'DX',
  'season_year': 2026,
  'sow_from': '2026-12-25',
  'sow_to': '2027-01-05',
  'can_manage': false,
  'updated_at': '2026-11-19T00:00:00Z',
};

void main() {
  test(
    'calendar prefill keeps year/frame first day and rejects impossible dates',
    () {
      final p = SeasonCalendarPrefill.fromJson(calendar());
      expect(p.displayAnchor, '25/12/2026');
      expect(p.year, 2026);
      expect(p.cropCode, 'LUA');
      expect(
        () => SeasonCalendarPrefill.fromJson({
          ...calendar(),
          'sow_from': '2026-02-31',
        }),
        throwsFormatException,
      );
    },
  );
  testWidgets(
    'AH-59: Member reads calendar, opens household with editable first day',
    (tester) async {
      tester.view.physicalSize = const Size(800, 1800);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.reset);
      final env = householdEnv('htx_member');
      final base = env.server.onGet!;
      env.server.onGet = (path, query) async {
        if (path == '/v1/cooperatives/$coopRef/season-calendars') {
          return {
            'items': [calendar()],
            'next_cursor': null,
          };
        }
        if (path == '/v1/cropcycle/calendars/$calendarRef') return calendar();
        return base(path, query);
      };
      await loginToHome(tester, env);
      await openTile(tester, 'Lịch thời vụ');
      await tapText(tester, 'Xem lịch');
      await tapText(tester, 'Mở vụ theo lịch');
      await tapText(tester, 'Hộ thử nghiệm');
      expect(find.text('Khai báo vụ mới'), findsOneWidget);
      await tapText(tester, 'Tiếp tục');
      await choose(tester, 'Giống', 'OM18');
      await tester.enterText(
        find.widgetWithText(TextField, 'Diện tích vụ (ha)'),
        '0.20',
      );
      await tapText(tester, 'Tiếp tục');
      final anchor = find.widgetWithText(TextField, 'Mốc gốc (dd/mm/yyyy)');
      expect(tester.widget<TextField>(anchor).controller!.text, '25/12/2026');
      await tester.enterText(anchor, '01/02/2027');
      await tapText(tester, 'Tiếp tục');
      final previews = env.server.sendCalls
          .where((r) => r.$2.endsWith('/season-plan-preview'))
          .toList();
      expect(previews.last.$4?['anchor_date'], '2027-02-01');
      expect(
        env.server.sendCalls.where((r) => r.$2.contains('season-calendars')),
        isEmpty,
      );
      await finish(tester);
    },
  );
  testWidgets('AH-59: cancelled deep link cannot open a season', (
    tester,
  ) async {
    final env = householdEnv('htx_member');
    final base = env.server.onGet!;
    env.server.onGet = (path, query) async {
      if (path == '/v1/cooperatives/$coopRef/season-calendars') {
        return {
          'items': [calendar()],
          'next_cursor': null,
        };
      }
      if (path == '/v1/cropcycle/calendars/$calendarRef') {
        throw ApiException(404, 'cropcycle.calendar_not_found', 'Lịch đã hủy.');
      }
      return base(path, query);
    };
    await loginToHome(tester, env);
    await openTile(tester, 'Lịch thời vụ');
    await tapText(tester, 'Xem lịch');
    expect(find.text('Lịch đã hủy.'), findsOneWidget);
    expect(find.text('Mở vụ theo lịch'), findsNothing);
    await finish(tester);
  });
  testWidgets('AH-59: representative has no calendar tile on mobile', (
    tester,
  ) async {
    final env = householdEnv('coop_manager');
    await loginToHome(tester, env);
    expect(find.text('Lịch thời vụ'), findsNothing);
    await finish(tester);
  });
}
