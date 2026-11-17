import 'package:agrigreen_mobile/application/inbox_service.dart';
import 'package:agrigreen_mobile/domain/deep_link.dart';
import 'package:flutter_test/flutter_test.dart';

InboxItem _item(
  String id,
  DateTime? createdAt, {
  String kind = 'season_task_due',
}) => InboxItem(
  id: id,
  kind: kind,
  severity: 'normal',
  title: 'Thông báo $id',
  body: 'Nội dung',
  deepLink: null,
  createdAt: createdAt,
  read: false,
);

void main() {
  // 10:00 ngày 01/10/2026 giờ Việt Nam.
  final now = DateTime.utc(2026, 10, 1, 3);

  test('AH-22 AC1: nhóm theo ngày giờ Việt Nam, nhãn và thứ tự', () {
    final days = groupInboxByDay([
      _item('a', DateTime.utc(2026, 9, 30, 20)), // 03:00 01/10
      _item('b', DateTime.utc(2026, 9, 30, 17, 30)), // 00:30 01/10 (biên ngày)
      _item('c', DateTime.utc(2026, 9, 30, 5)), // 12:00 30/09
      _item('d', DateTime.utc(2026, 9, 27, 1)),
      _item('e', null),
    ], now);
    expect(days.map((d) => d.label), [
      'Hôm nay',
      'Hôm qua',
      '27/09/2026',
      'Không rõ ngày',
    ]);
    expect(days[0].items.map((i) => i.id), ['a', 'b']);
    expect(days[1].items.map((i) => i.id), ['c']);
    expect(groupInboxByDay([], now), isEmpty);
  });

  test('AH-22 AC1: giờ tạo theo giờ Việt Nam; nhãn bốn loại mới', () {
    expect(inboxTime(DateTime.utc(2026, 9, 30, 23, 30)), '06:30');
    expect(inboxTime(DateTime.utc(2026, 10, 1, 3, 5)), '10:05');
    expect(
      _item('x', null, kind: 'season_calendar_published').kindLabel,
      'Lịch thời vụ',
    );
    expect(_item('x', null, kind: 'season_task_due').kindLabel, 'Nhắc việc');
    expect(
      _item('x', null, kind: 'season_log_missing').kindLabel,
      'Nhắc ghi nhật ký',
    );
    expect(
      _item('x', null, kind: 'season_process_changed').kindLabel,
      'Quy trình thay đổi',
    );
  });

  test(
    'AH-22 AC2: deep link việc → vụ kèm việc; vụ; lịch chưa có màn đích',
    () {
      expect(
        DeepLink.parse('/cropcycle/seasons/s1/tasks/t1'),
        const SeasonLink('s1', taskRef: 't1'),
      );
      expect(DeepLink.parse('/cropcycle/seasons/s1'), const SeasonLink('s1'));
      expect(
        DeepLink.parse('/cropcycle/seasons/s1/tasks/t1') ==
            const SeasonLink('s1'),
        isFalse,
      );
      expect(DeepLink.parse('/cropcycle/seasons/s1/tasks/'), isA<InboxLink>());
      expect(
        DeepLink.parse('/cropcycle/seasons/s1/tasks/../x'),
        isA<InboxLink>(),
      );
      expect(DeepLink.parse('/cropcycle/calendars/c1'), isA<InboxLink>());
    },
  );

  test(
    'AH-303: deep link thu gom rơm (lịch thu gom, bảng kê) → màn thu gom',
    () {
      expect(DeepLink.parse('/htx/thu-gom'), const CollectionLink());
      expect(DeepLink.parse('/htx/thu-gom/x'), const InboxLink());
      expect(DeepLink.parse('/htx/khac'), const InboxLink());
    },
  );
}
