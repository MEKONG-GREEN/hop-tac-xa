import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/test_env.dart';

Map<String, Object?> _coop() => {
  'ref': coopRef,
  'name': 'HTX Tân Phú',
  'code': 'HTX-00012',
  'active': true,
  'tax_code': '0312345678',
  'commune_name': 'Xã Tân Phú',
  'province_name': 'Cần Thơ',
  'household_areas': {
    'households': 42,
    'cultivated_ha': '120.5000',
    'livestock_ha': '3.2500',
    'missing_cultivated': 0,
    'missing_livestock': 0,
  },
  'machine_count': 7,
};

void main() {
  for (final role in const ['htx_member', 'coop_manager']) {
    testWidgets('AH-284: $role xem thông tin HTX của mình, chỉ đọc', (
      tester,
    ) async {
      final env = TestEnv(roles: [role])..seed();
      final base = env.server.onGet!;
      var online = true;
      env.server.onGet = (path, q) async {
        if (path == '/v1/cooperatives/$coopRef') {
          if (!online) throw NetworkException('offline');
          return _coop();
        }
        return base(path, q);
      };
      await loginToHome(tester, env);
      await openTile(tester, 'Thông tin HTX');
      expect(find.text('HTX Tân Phú'), findsWidgets);
      expect(find.text('HTX-00012'), findsOneWidget);
      expect(find.text('Xã Tân Phú, Cần Thơ'), findsOneWidget);
      expect(find.text('42'), findsOneWidget);
      expect(find.text('120,50 ha'), findsOneWidget);
      expect(find.text('3,25 ha'), findsOneWidget);
      expect(find.text('7'), findsOneWidget);
      expect(find.text('Chỉ xem. Sửa thông tin HTX trên web.'), findsOneWidget);
      // Không có thao tác sửa, xóa, chia sẻ trên app.
      expect(find.byType(FilledButton), findsNothing);
      expect(find.byType(ElevatedButton), findsNothing);

      // Ngoại tuyến: dùng bản đã lưu, báo rõ đang xem bản lưu.
      online = false;
      await tester.pageBack();
      await settle(tester);
      await openTile(tester, 'Thông tin HTX');
      expect(find.text('HTX-00012'), findsOneWidget);
      expect(
        find.text('Đang xem thông tin đã lưu (chưa có mạng).'),
        findsOneWidget,
      );
    });
  }

  testWidgets('AH-284: nông dân không có ô Thông tin HTX', (tester) async {
    final env = TestEnv(roles: const ['farmer'])..seed();
    await loginToHome(tester, env);
    expect(find.text('Thông tin HTX', skipOffstage: false), findsNothing);
  });
}
