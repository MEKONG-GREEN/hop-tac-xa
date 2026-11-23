import '../domain/roles.dart';

/// A session-local authorization choice; the original JWT is never modified.
class HtxWorkingRole {
  HtxWorkingRole({required this.enabled, required Iterable<String> roles})
    : available = _htxRoles(roles) {
    if (enabled && available.length == 1) selected = available.single;
  }

  final bool enabled;

  /// Vai App HTX trong token hiện tại (đọc lại khi vai bị ngừng — [reset]).
  List<String> available;
  String? selected;

  /// FN-TK-01/EX-03 (AH-222): câu báo vai vừa bị ngừng; null khi không có.
  String? notice;

  bool get needsSelection =>
      enabled && available.length > 1 && selected == null;

  static List<String> _htxRoles(Iterable<String> roles) => [
    for (final role in [HtxRole.admin, HtxRole.coopManager, HtxRole.htxMember])
      if (roles.contains(role)) role,
  ];

  void select(String role) {
    if (!enabled || !available.contains(role)) {
      throw StateError('Vai trò không thuộc tài khoản');
    }
    selected = role;
    notice = null;
  }

  void chooseAgain() {
    if (enabled) {
      selected = null;
      notice = null;
    }
  }

  /// FN-TK-01/EX-03 (AH-222): token đã làm mới mang [roles]. Vai đang chọn không
  /// còn thì bỏ lựa chọn: còn một vai → dùng luôn vai đó (FN-TK-01/AC-01), còn
  /// hai vai → phải chọn lại.
  void reset(Iterable<String> roles) {
    if (!enabled) return;
    available = _htxRoles(roles);
    if (selected != null && available.contains(selected)) return;
    selected = available.length == 1 ? available.single : null;
  }
}
