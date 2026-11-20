import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/test_env.dart';

void main() {
  testWidgets(
    'AH-285: Member xem bản che, chỉ đọc; mất quyền không dùng hồ sơ cũ',
    (tester) async {
      final env = TestEnv(roles: const ['htx_member'])..seed();
      final base = env.server.onGet!;
      var denied = false;
      env.server.onGet = (path, query) async {
        if (path == '/v1/cooperatives/$coopRef/households') {
          return {
            'items': [
              {
                'household_ref': householdRef,
                'label': 'Hộ thử nghiệm',
                'code': 'H-285',
                'cultivated_area_ha': '2.5',
                'active_area_ha': '0',
                'remaining_area_ha': '2.5',
                'can_open_season': true,
              },
            ],
            'next_cursor': null,
          };
        }
        if (path == '/v1/households/$householdRef/profile') {
          if (denied) throw ApiException(403, 'forbidden');
          return {
            'code': 'H-285',
            'revision': 1,
            'contact_access': 'masked',
            'household': {
              'household_ref': householdRef,
              'cooperative_ref': coopRef,
              'active': true,
              'cultivated_area_ha': '2.5',
              'livestock_area_ha': '0.5',
            },
            'identity': {
              'full_name': 'Chủ hộ thử nghiệm',
              'phone': '0907***091',
              'national_id': '079******789',
              'address': 'Ấp thử nghiệm',
            },
          };
        }
        return base(path, query);
      };
      await loginToHome(tester, env);
      await openTile(tester, 'Nông hộ');
      await tester.tap(find.byIcon(Icons.info_outline).first);
      await tester.pumpAndSettle();
      expect(find.text('Chi tiết hộ xã viên'), findsOneWidget);
      expect(find.text('079******789'), findsOneWidget);
      expect(find.text('0907***091'), findsOneWidget);
      for (final label in ['Thêm hộ', 'Sửa hộ', 'Xóa hộ']) {
        expect(find.text(label), findsNothing);
      }
      await tester.pageBack();
      await tester.pumpAndSettle();
      denied = true;
      await tester.tap(find.byIcon(Icons.info_outline).first);
      await tester.pumpAndSettle();
      expect(find.text('079******789'), findsNothing);
      expect(find.textContaining('không còn quyền truy cập'), findsOneWidget);
      await finish(tester);
    },
  );
}
