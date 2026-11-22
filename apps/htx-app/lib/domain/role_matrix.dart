import 'dart:convert';

import 'generated/htx_roles.dart';

/// Hành động trong ma trận quyền App HTX (khóa `actions` của
/// `contracts/htxroles/htx-roles.v1.json`). App chỉ dùng bốn hành động CRUD.
enum HtxAction {
  create('C'),
  read('R'),
  update('U'),
  delete('D');

  const HtxAction(this.code);
  final String code;
}

/// Mã chức năng của ma trận mà app di động dùng để ẩn/hiện màn và nút.
abstract final class HtxFn {
  /// Quản lý HTX (FN-03): app chỉ xem thông tin HTX của mình.
  static const cooperative = 'FN-03';

  /// Quản lý hộ xã viên (Mục V).
  static const households = 'FN-04';

  /// Xem danh sách nông hộ và diện tích đất (KHMV).
  static const plotsAndArea = 'KHMV-FN-02';

  /// Khai báo mở vụ mới; D = hủy vụ.
  static const season = 'KHMV-FN-03';
  static const calendar = 'KHMV-FN-03-LICH';

  /// Xem chi tiết vụ, tiến độ, kế hoạch mùa vụ.
  static const seasonView = 'KHMV-FN-03-XEM';

  /// Ghi nhật ký canh tác.
  static const farmLog = 'KHMV-FN-05';

  /// Khai báo sản lượng.
  static const harvest = 'KHMV-FN-06';
}

/// Ma trận quyền App HTX (FN-05, Mục V và dòng KHMV-*), đọc từ bản sinh của
/// `contracts/htxroles/htx-roles.v1.json` — app không giữ bản ma trận riêng.
/// Cùng quy tắc với `htxroles` của máy chủ và `permissions.ts` của web. CHỈ
/// dùng để ẩn/hiện; máy chủ kiểm lại mọi yêu cầu (FN-05/BR-03, TECH-01).
class HtxRoleMatrix {
  HtxRoleMatrix._(this.roles, this._grants);

  factory HtxRoleMatrix.parse(String json) {
    final m = jsonDecode(json) as Map<String, Object?>;
    final roles = [
      for (final r in m['roles']! as List<Object?>)
        (r! as Map<String, Object?>)['key']! as String,
    ];
    final grants = <String, Map<String, String>>{};
    for (final f in m['functions']! as List<Object?>) {
      final row = f! as Map<String, Object?>;
      final g = row['grants']! as Map<String, Object?>;
      grants[row['key']! as String] = {
        for (final e in g.entries) e.key: e.value! as String,
      };
    }
    return HtxRoleMatrix._(List.unmodifiable(roles), grants);
  }

  /// Vai của ma trận theo thứ tự ưu tiên: Admin, Đại diện HTX, Thành viên HTX.
  final List<String> roles;
  final Map<String, Map<String, String>> _grants;

  bool hasFunction(String fn) => _grants.containsKey(fn);

  /// Chuỗi quyền (vd `CRUD`) của [role] với [fn]; `''` khi không có.
  String grants(String role, String fn) => _grants[fn]?[role] ?? '';

  bool can(String role, String fn, HtxAction action) =>
      grants(role, fn).contains(action.code);

  /// Vai đang có hiệu lực: vai đầu tiên theo thứ tự ma trận mà tài khoản có;
  /// null khi không có vai nào. Quyền không cộng gộp khi kiêm hai vai (BRD
  /// mục V) — như `htxroles.Effective` và `effectiveRole` của web.
  String? effectiveRole(Iterable<String> has) {
    final set = has.toSet();
    for (final r in roles) {
      if (set.contains(r)) return r;
    }
    return null;
  }
}

/// Ma trận dùng chung của app (biến top-level: khởi tạo lười lần đầu dùng).
final htxRoleMatrix = HtxRoleMatrix.parse(htxRoleMatrixJson);
