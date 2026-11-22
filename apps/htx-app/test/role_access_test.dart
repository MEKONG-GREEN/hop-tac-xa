import 'package:agrigreen_htx/ui/navigation.dart';
import 'package:agrigreen_htx/ui/screens/collection_screen.dart';
import 'package:agrigreen_htx/ui/screens/home_screen.dart';
import 'package:agrigreen_htx/ui/screens/news_screen.dart';
import 'package:agrigreen_htx/ui/screens/plots_screen.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/test_env.dart';

/// Ô trên trang chủ: Thành viên HTX (ma trận KHMV, quyết định 17/11/2026).
const _memberTiles = [
  'Thông tin HTX',
  'Nông hộ',
  'Bản đồ lô ruộng',
  'Lịch thời vụ',
  'Mở vụ hàng loạt',
  'Ghi nhật ký hộ',
  'Nhật ký hàng loạt',
  'Khai sản lượng',
  'Tin tức',
  'Hàng đợi đồng bộ',
];

/// Ô trên trang chủ: Đại diện HTX (chỉ xem dữ liệu ma trận, giữ chức năng
/// ngoài ma trận).
const _managerTiles = [
  'Thông tin HTX',
  'Nông hộ',
  'Bản đồ lô ruộng',
  'Xem vụ và nhật ký hộ',
  'Duyệt nhật ký',
  'Thu gom rơm MKG',
  'Tổng quan',
  'Tin tức',
  'Hàng đợi đồng bộ',
];

/// Nhãn các ô đang dựng trên trang chủ, theo thứ tự.
List<String> _tiles(WidgetTester tester) => [
  for (final t in tester.widgetList<BigTile>(
    find.byType(BigTile, skipOffstage: false),
  ))
    t.label,
];

Finder _all(String text) => find.text(text, skipOffstage: false);

/// Chữ trong màn chi tiết lô (trang chủ bên dưới cũng có ô cùng nhãn).
Finder _inPlot(String text) => find.descendant(
  of: find.byType(PlotDetailScreen, skipOffstage: false),
  matching: _all(text),
);

/// Môi trường có danh sách nông hộ của HTX.
TestEnv _env(List<String> roles) {
  final env = TestEnv(roles: roles)..seed(season: true);
  final base = env.server.onGet!;
  env.server.onGet = (path, q) async =>
      path == '/v1/cooperatives/$coopRef/households'
      ? {
          'items': [
            {'ref': householdRef, 'label': 'Nguyễn V. A.', 'code': 'HH-01'},
          ],
          'next_cursor': null,
        }
      : base(path, q);
  return env;
}

Future<void> _back(WidgetTester tester) async {
  await tester.pageBack();
  await settle(tester);
}

void main() {
  testWidgets('[FN-05/AC-01, AH-59] Thành viên HTX: trang chủ đúng 9 ô; nông hộ và bản đồ chỉ '
      'xem (không gọi, không thêm/sửa/xóa, không sửa ranh); chi tiết lô có nút '
      'ghi nhật ký, khai sản lượng, sửa/hủy vụ', (tester) async {
    final env = _env(const ['htx_member']);
    await loginToHome(tester, env);
    expect(_tiles(tester), _memberTiles);
    expect(_all('AgriGreen HTX · Thành viên HTX'), findsOneWidget);

    await openTile(tester, 'Nông hộ');
    expect(find.text('Nguyễn V. A.'), findsOneWidget);
    expect(find.byIcon(Icons.phone, skipOffstage: false), findsNothing);
    for (final icon in [Icons.add, Icons.edit, Icons.delete]) {
      expect(find.byIcon(icon, skipOffstage: false), findsNothing);
    }
    for (final label in ['Thêm', 'Sửa', 'Xóa']) {
      expect(_all(label), findsNothing);
    }
    await _back(tester);

    await openTile(tester, 'Bản đồ lô ruộng');
    expect(find.textContaining('T1', skipOffstage: false), findsWidgets);
    expect(_all('Vẽ lô mới'), findsNothing);
    expect(_all('Vẽ ranh giới'), findsNothing);
    expect(_all('Sửa ranh giới'), findsNothing);
    await _back(tester);

    await openTile(tester, 'Ghi nhật ký hộ');
    await tapText(tester, 'T1 · Ruộng bờ kênh');
    for (final label in ['Ghi nhật ký', 'Khai sản lượng', 'Sửa vụ', 'Hủy vụ']) {
      expect(_inPlot(label), findsOneWidget, reason: label);
    }
    await finish(tester);
  });

  testWidgets(
    'Đại diện HTX: không có ô ghi; xem vụ và nhật ký hộ không có nút thêm, '
    'sửa, xóa; giữ sửa ranh thửa, duyệt nhật ký, thu gom rơm',
    (tester) async {
      final env = _env(const ['coop_manager']);
      await loginToHome(tester, env);
      expect(_tiles(tester), _managerTiles);
      for (final label in [
        'Mở vụ hàng loạt',
        'Nhật ký hàng loạt',
        'Khai sản lượng',
      ]) {
        expect(_all(label), findsNothing, reason: label);
      }

      await openTile(tester, 'Xem vụ và nhật ký hộ');
      await tapText(tester, 'T1 · Ruộng bờ kênh');
      expect(find.text('Vụ TD 2026 · giống OM18'), findsOneWidget);
      for (final label in [
        'Ghi nhật ký',
        'Khai sản lượng',
        'Sửa vụ',
        'Hủy vụ',
        'Mở vụ mới',
      ]) {
        expect(_inPlot(label), findsNothing, reason: label);
      }
      await _back(tester);
      await _back(tester);

      await openTile(tester, 'Bản đồ lô ruộng');
      expect(_all('Vẽ lô mới'), findsOneWidget);
      await finish(tester);
    },
  );

  testWidgets('kiêm Đại diện HTX và Thành viên HTX: dùng quyền Đại diện HTX', (
    tester,
  ) async {
    final env = _env(const ['coop_manager', 'htx_member']);
    await loginToHome(tester, env);
    expect(_tiles(tester), _managerTiles);
    await finish(tester);
  });

  testWidgets(
    '[FN-05/AC-03, EX-01] Thành viên HTX chạm liên kết tới màn không được xem '
    '(thu gom rơm) → về trang chủ theo vai kèm câu báo',
    (tester) async {
      final env = _env(const ['htx_member']);
      await loginToHome(tester, env);
      await openTile(tester, 'Tin tức');
      expect(find.byType(NewsScreen), findsOneWidget);

      env.notifier.tap('/htx/thu-gom');
      await settle(tester);
      expect(find.byType(NewsScreen), findsNothing);
      expect(find.byType(CollectionScreen), findsNothing);
      expect(find.byType(HomeScreen), findsOneWidget);
      expect(find.text(msgNoAccess), findsOneWidget);
      await finish(tester);
    },
  );

  testWidgets('Đại diện HTX chạm liên kết thu gom rơm → màn Thu gom rơm MKG', (
    tester,
  ) async {
    final env = _env(const ['coop_manager']);
    await loginToHome(tester, env);
    env.notifier.tap('/htx/thu-gom');
    await settle(tester);
    expect(find.byType(CollectionScreen), findsOneWidget);
    expect(find.text(msgNoAccess), findsNothing);
    await finish(tester);
  });
}
