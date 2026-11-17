import 'dart:convert';
import 'dart:io';

import 'package:agrigreen_htx/domain/generated/htx_roles.dart';
import 'package:agrigreen_htx/domain/role_matrix.dart';
import 'package:agrigreen_htx/domain/roles.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('AH-303: ma trận Dart sinh từ contracts/htxroles', () {
    test('bản sinh trùng tệp JSON gốc (app không giữ bản ma trận khác)', () {
      final source = File('../../contracts/htxroles/htx-roles.v1.json')
          .readAsStringSync();
      expect(
        jsonEncode(jsonDecode(htxRoleMatrixJson)),
        jsonEncode(jsonDecode(source)),
        reason: 'chạy `npm run gen:modules` trong apps/web-htx để sinh lại bản Dart',
      );
    });

    test('khóa chức năng app dùng có trong ma trận; thứ tự vai', () {
      for (final fn in [
        HtxFn.households,
        HtxFn.plotsAndArea,
        HtxFn.season,
        HtxFn.seasonView,
        HtxFn.farmLog,
        HtxFn.harvest,
      ]) {
        expect(htxRoleMatrix.hasFunction(fn), isTrue, reason: fn);
      }
      expect(htxRoleMatrix.roles, [
        'platform_admin',
        'coop_manager',
        'htx_member',
      ]);
      expect(htxRoleMatrix.grants('htx_member', 'KHÔNG-CÓ'), '');
    });
  });

  test(
    'modeFor theo vai có hiệu lực (Admin > Đại diện HTX > Thành viên HTX)',
    () {
      expect(modeFor(['htx_member']), HtxMode.member);
      expect(modeFor(['coop_manager', 'htx_member']), HtxMode.manager);
      expect(modeFor(['platform_admin', 'coop_manager']), HtxMode.admin);
      expect(modeFor(['farmer', 'htx_member']), HtxMode.member);
      expect(modeFor(['farmer']), HtxMode.farmer);
      expect(modeFor(['farmer', 'coop_manager']), HtxMode.manager);
      expect(modeFor(['kn_officer']), HtxMode.none);
      expect(modeFor(const []), HtxMode.none);
    },
  );

  test('[FN-05/AC-01] Thành viên HTX không có quyền thêm, sửa, xóa hồ sơ HTX, '
      'hộ và tài khoản; có quyền mở vụ, ghi nhật ký, khai sản lượng', () {
    const m = HtxMode.member;
    for (final fn in ['FN-03', 'FN-04']) {
      for (final a in [HtxAction.create, HtxAction.update, HtxAction.delete]) {
        expect(m.can(fn, a), isFalse, reason: '$fn ${a.code}');
      }
    }
    for (final fn in ['FN-07', 'FN-08']) {
      for (final a in HtxAction.values) {
        expect(m.can(fn, a), isFalse, reason: '$fn ${a.code}');
      }
      expect(htxRoleMatrix.grants('htx_member', fn), '', reason: '$fn (L)');
    }
    expect(m.can(HtxFn.season, HtxAction.create), isTrue);
    expect(m.can(HtxFn.farmLog, HtxAction.create), isTrue);
    expect(m.can(HtxFn.harvest, HtxAction.create), isTrue);
  });

  test('Đại diện HTX chỉ xem vụ; nông dân ngoài ma trận; none không có gì', () {
    expect(HtxMode.manager.can(HtxFn.season, HtxAction.create), isFalse);
    expect(HtxMode.manager.can(HtxFn.seasonView, HtxAction.read), isTrue);
    expect(HtxMode.farmer.can(HtxFn.season, HtxAction.create), isTrue);
    expect(HtxMode.none.can(HtxFn.seasonView, HtxAction.read), isFalse);
  });

  test('canView theo từng chế độ; Admin và none bị chặn', () {
    const common = {
      HtxScreen.notifications,
      HtxScreen.news,
      HtxScreen.syncQueue,
    };
    final expected = <HtxMode, Set<HtxScreen>>{
      HtxMode.farmer: {
        ...common,
        HtxScreen.farmerPlots,
        HtxScreen.farmLog,
        HtxScreen.support,
        HtxScreen.staff,
        HtxScreen.harvest,
        HtxScreen.plotDetail,
        HtxScreen.farmLogDetail,
      },
      HtxMode.manager: {
        ...common,
        HtxScreen.households,
        HtxScreen.plotMap,
        HtxScreen.householdLogs,
        HtxScreen.plotDetail,
        HtxScreen.farmLogDetail,
        HtxScreen.review,
        HtxScreen.collection,
        HtxScreen.dashboard,
      },
      HtxMode.member: {
        ...common,
        HtxScreen.households,
        HtxScreen.plotMap,
        HtxScreen.bulkOpen,
        HtxScreen.householdLogs,
        HtxScreen.batchLog,
        HtxScreen.harvest,
        HtxScreen.plotDetail,
        HtxScreen.farmLogDetail,
      },
      HtxMode.admin: const {},
      HtxMode.none: const {},
    };
    for (final mode in HtxMode.values) {
      expect(
        {
          for (final s in HtxScreen.values)
            if (mode.canView(s)) s,
        },
        expected[mode],
        reason: mode.name,
      );
    }
    expect(HtxMode.admin.blocked, isTrue);
    expect(HtxMode.none.blocked, isTrue);
    expect(HtxMode.member.blocked, isFalse);
    expect(HtxMode.manager.editsBoundaries, isTrue);
    expect(HtxMode.member.editsBoundaries, isFalse);
    expect(HtxMode.member.callsHouseholds, isFalse);
  });
}
