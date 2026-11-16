import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/test_env.dart';

/// AH-24 AC4: nhãn ô lối tắt ở màn hình chủ là tiếng Việt và tiêu đề thanh trên
/// cùng của màn mỗi ô mở ra trùng nhãn ô.
Future<void> _expectTitlesMatchTiles(
  WidgetTester tester,
  List<String> roles,
  List<String> labels,
) async {
  final env = TestEnv(roles: roles)..seed();
  await loginToHome(tester, env);
  expect(find.text('Dashboard'), findsNothing);
  for (final label in labels) {
    await openTile(tester, label);
    expect(
      find.descendant(of: find.byType(AppBar), matching: find.text(label)),
      findsOneWidget,
      reason: 'tiêu đề màn của ô "$label"',
    );
    await tester.pageBack();
    await settle(tester);
  }
}

void main() {
  testWidgets('Thành viên HTX: tiêu đề màn trùng nhãn ô lối tắt', (
    tester,
  ) async {
    await _expectTitlesMatchTiles(
      tester,
      const ['farmer'],
      const [
        'Ruộng của tôi',
        'Ghi nhật ký',
        'Khai sản lượng',
        'Yêu cầu hỗ trợ',
        'Danh bạ cán bộ',
        'Tin tức',
        'Hàng đợi đồng bộ',
      ],
    );
  });

  testWidgets('Đại diện HTX: tiêu đề màn trùng nhãn ô lối tắt', (tester) async {
    await _expectTitlesMatchTiles(
      tester,
      const ['coop_manager'],
      const [
        'Nông hộ',
        'Bản đồ lô ruộng',
        'Mở vụ hàng loạt',
        'Ghi nhật ký hộ',
        'Nhật ký hàng loạt',
        'Duyệt nhật ký',
        'Thu gom rơm MKG',
        'Tổng quan',
        'Tin tức',
        'Hàng đợi đồng bộ',
      ],
    );
  });
}
