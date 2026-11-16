import 'package:agrigreen_htx/ui/screens/notifications_screen.dart';
import 'package:agrigreen_htx/ui/screens/plots_screen.dart';
import 'package:agrigreen_mobile/data/actions/action_queue.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/test_env.dart';

const _season = '01920000-0000-7000-8000-00000000e001';
const _task = '01920000-0000-7000-8000-00000000e0a1';
const _calendar = '01920000-0000-7000-8000-00000000ca11';

Map<String, Object?> _notice(
  String id,
  String kind,
  String title,
  String link,
  String createdAt, {
  String? readAt,
}) => {
  'entity': 'notification',
  'id': id,
  'op': 'upsert',
  'data': {
    'id': id,
    'kind': kind,
    'severity': 'normal',
    'title': title,
    'body': 'Nội dung',
    'deep_link': link,
    'created_at': createdAt,
    'read_at': ?readAt,
  },
};

// testNow = 2026-10-01T03:00Z = 10:00 ngày 01/10 giờ Việt Nam.
List<Map<String, Object?>> _notices() => [
  _notice(
    'n1',
    'season_task_due',
    'Việc đến hạn: Bón phân',
    '/cropcycle/seasons/$_season/tasks/$_task',
    '2026-10-01T01:00:00Z',
  ),
  _notice(
    'n2',
    'season_log_missing',
    'Vụ đã 4 ngày chưa có nhật ký',
    '/cropcycle/seasons/$_season',
    '2026-09-30T05:00:00Z',
    readAt: '2026-09-30T06:00:00Z',
  ),
  _notice(
    'n3',
    'season_calendar_published',
    'Lịch thời vụ mới: Lúa, vụ Đông Xuân/2026',
    '/cropcycle/calendars/$_calendar',
    '2026-09-28T05:00:00Z',
  ),
];

Future<void> _tall(WidgetTester tester) async {
  tester.view.physicalSize = const Size(800, 2400);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.reset);
}

String _stateOf(WidgetTester tester, String id) =>
    tester.widget<Text>(find.byKey(Key('read-state-$id'))).data!;

void main() {
  testWidgets(
    'AH-22 AC1: trung tâm thông báo nhóm theo ngày, giờ tạo, trạng thái đọc, '
    'lọc chưa đọc, Đọc tất cả',
    (tester) async {
      await _tall(tester);
      final env = TestEnv(roles: const ['coop_manager'])
        ..seed(season: true, extra: _notices());
      final s = await loginToHome(tester, env);
      await tapOn(tester, find.byTooltip('Thông báo'));

      expect(find.text('Hôm nay'), findsOneWidget);
      expect(find.text('Hôm qua'), findsOneWidget);
      expect(find.text('28/09/2026'), findsOneWidget);
      expect(_stateOf(tester, 'n1'), '08:00 · Chưa đọc');
      expect(_stateOf(tester, 'n2'), '12:00 · Đã đọc');
      expect(_stateOf(tester, 'n3'), '12:00 · Chưa đọc');

      // Bộ lọc chưa đọc ẩn mục đã đọc.
      await tapText(tester, 'Chỉ chưa đọc');
      expect(find.text('Vụ đã 4 ngày chưa có nhật ký'), findsNothing);
      expect(find.text('Việc đến hạn: Bón phân'), findsOneWidget);
      await tapText(tester, 'Chỉ chưa đọc');
      expect(find.text('Vụ đã 4 ngày chưa có nhật ký'), findsOneWidget);

      // Đọc tất cả: mọi mục chuyển đã đọc, lệnh được xếp hàng.
      await tapText(tester, 'Đọc tất cả');
      await settle(tester);
      expect(_stateOf(tester, 'n1'), endsWith('Đã đọc'));
      expect(_stateOf(tester, 'n3'), endsWith('Đã đọc'));
      expect(
        (await s.actions.ofKind(ApiActionKind.notificationReadAll)).length,
        1,
      );
      expect(await s.inbox.unreadCount(), 0);
      await finish(tester);
    },
  );

  testWidgets(
    'AH-22 AC2: bấm thông báo việc → đánh dấu đã đọc và mở vụ liên quan',
    (tester) async {
      await _tall(tester);
      final env = TestEnv(roles: const ['coop_manager'])
        ..seed(season: true, extra: _notices());
      final s = await loginToHome(tester, env);
      await tapOn(tester, find.byTooltip('Thông báo'));
      await tapText(tester, 'Việc đến hạn: Bón phân');
      await settle(tester);
      expect(find.byType(PlotDetailScreen), findsOneWidget);
      expect(
        (await s.actions.ofKind(ApiActionKind.notificationRead)).single.target,
        'n1',
      );
      await finish(tester);
    },
  );

  testWidgets(
    'AH-22 AC2: bấm thông báo nhật ký (NTF.3) → màn vụ; thông báo lịch chưa có '
    'màn đích → ở lại trung tâm, ghi "Chưa có màn hình liên kết"',
    (tester) async {
      await _tall(tester);
      final env = TestEnv(roles: const ['coop_manager'])
        ..seed(season: true, extra: _notices());
      await loginToHome(tester, env);
      await tapOn(tester, find.byTooltip('Thông báo'));

      expect(find.text('Chưa có màn hình liên kết'), findsOneWidget);
      await tapText(tester, 'Lịch thời vụ mới: Lúa, vụ Đông Xuân/2026');
      await settle(tester);
      expect(find.byType(NotificationsScreen), findsOneWidget);
      expect(find.byType(PlotDetailScreen), findsNothing);

      await tapText(tester, 'Vụ đã 4 ngày chưa có nhật ký');
      await settle(tester);
      expect(find.byType(PlotDetailScreen), findsOneWidget);
      await finish(tester);
    },
  );
}
