import 'dart:convert';

import 'role_matrix.dart';

/// Vai của App HTX (g2-conventions §5, docs/conventions/vai-tro-app-htx.md).
/// Quyền thật kiểm ở backend (R-55, TECH-01); app chỉ chọn trang chủ và
/// ẩn/hiện chức năng theo vai.
abstract final class HtxRole {
  static const farmer = 'farmer';

  /// Admin của App HTX — chỉ dùng web.
  static const admin = 'platform_admin';

  /// Đại diện HTX.
  static const coopManager = 'coop_manager';

  /// Thành viên HTX (AH-272).
  static const htxMember = 'htx_member';
}

enum HtxMode {
  /// Nông dân: ruộng của hộ mình (ngoài ma trận; AH-309 sẽ gỡ).
  farmer,

  /// Đại diện HTX: chỉ xem dữ liệu ma trận (vụ, nhật ký, sản lượng), giữ các
  /// chức năng ngoài ma trận đang có (duyệt nhật ký, thu gom rơm…).
  manager,

  /// Thành viên HTX: mở vụ, ghi nhật ký, khai sản lượng cho HTX mình.
  member,

  /// Admin: chỉ dùng web — app hiện màn chặn, không đồng bộ.
  admin,

  /// Tài khoản không có vai nào của App HTX — màn chặn, không đồng bộ.
  none,
}

/// Chế độ theo vai có hiệu lực của ma trận (Admin > Đại diện HTX > Thành viên
/// HTX); không có vai ma trận thì nông dân nếu có vai `farmer`.
HtxMode modeFor(Iterable<String> roles) {
  switch (htxRoleMatrix.effectiveRole(roles)) {
    case HtxRole.admin:
      return HtxMode.admin;
    case HtxRole.coopManager:
      return HtxMode.manager;
    case HtxRole.htxMember:
      return HtxMode.member;
  }
  return roles.contains(HtxRole.farmer) ? HtxMode.farmer : HtxMode.none;
}

/// Các màn của app, để quyết định màn nào được mở (trang chủ, liên kết từ
/// thông báo).
enum HtxScreen {
  notifications,
  news,
  syncQueue,
  farmerPlots,
  farmLog,
  support,
  staff,
  households,
  seasonCalendars,
  plotMap,
  bulkOpen,
  householdLogs,
  batchLog,
  harvest,
  plotDetail,
  farmLogDetail,
  review,
  collection,
  dashboard,
  cooperativeInfo,
}

/// Quy tắc ẩn/hiện của app theo chế độ — cùng ma trận quyền với máy chủ.
extension HtxModeAccess on HtxMode {
  /// Vai realm của ma trận ứng với chế độ; null với nông dân và `none`.
  String? get matrixRole => switch (this) {
    HtxMode.admin => HtxRole.admin,
    HtxMode.manager => HtxRole.coopManager,
    HtxMode.member => HtxRole.htxMember,
    HtxMode.farmer || HtxMode.none => null,
  };

  /// Admin và tài khoản không có vai: không vào được màn nghiệp vụ nào.
  bool get blocked => this == HtxMode.admin || this == HtxMode.none;

  /// Ma trận cho thao tác [action] với chức năng [fn] không.
  bool can(String fn, HtxAction action) => switch (this) {
    // Nông dân ngoài ma trận: giữ quyền như hiện tại.
    HtxMode.farmer => true,
    HtxMode.none => false,
    _ => htxRoleMatrix.can(matrixRole!, fn, action),
  };

  bool _matrix(String fn, HtxAction action) =>
      matrixRole != null && can(fn, action);

  bool canView(HtxScreen s) {
    if (blocked) return false;
    final farmer = this == HtxMode.farmer;
    return switch (s) {
      HtxScreen.notifications || HtxScreen.news || HtxScreen.syncQueue => true,
      HtxScreen.farmerPlots ||
      HtxScreen.farmLog ||
      HtxScreen.support ||
      HtxScreen.staff => farmer,
      HtxScreen.households => _matrix(HtxFn.households, HtxAction.read),
      // FN-03 (AH-284): app chỉ có màn xem thông tin HTX của mình, chỉ đọc.
      HtxScreen.cooperativeInfo => _matrix(HtxFn.cooperative, HtxAction.read),
      HtxScreen.seasonCalendars =>
        this == HtxMode.member && _matrix(HtxFn.calendar, HtxAction.read),
      HtxScreen.plotMap => _matrix(HtxFn.plotsAndArea, HtxAction.read),
      HtxScreen.bulkOpen => _matrix(HtxFn.season, HtxAction.create),
      HtxScreen.householdLogs => _matrix(HtxFn.seasonView, HtxAction.read),
      HtxScreen.batchLog => _matrix(HtxFn.farmLog, HtxAction.create),
      HtxScreen.harvest => farmer || can(HtxFn.harvest, HtxAction.create),
      HtxScreen.plotDetail => farmer || can(HtxFn.seasonView, HtxAction.read),
      HtxScreen.farmLogDetail => farmer || can(HtxFn.farmLog, HtxAction.read),
      // Ngoài ma trận: giữ quyền đang có của Đại diện HTX (quyết định 17/11,
      // PO quyết định sau theo bảng của AH-39).
      HtxScreen.review ||
      HtxScreen.collection ||
      HtxScreen.dashboard => this == HtxMode.manager,
    };
  }

  /// Sửa ranh thửa: ngoài 9 dòng KHMV, máy chủ chưa đổi quyền này — giữ cho
  /// Đại diện HTX.
  bool get editsBoundaries => this == HtxMode.manager;

  /// Gọi nông hộ (US-HH-03) ngoài ma trận: máy chủ chỉ trả số điện thoại cho
  /// vai ghi nông hộ (`householdWriterRoles`, không có `htx_member`).
  bool get callsHouseholds => this == HtxMode.manager;
}

/// Vai trong access token (claim `realm_access.roles` của Keycloak). Chỉ đọc
/// phần thân JWT để chọn giao diện khi ngoại tuyến — **không** xác minh chữ ký
/// (máy chủ xác minh mỗi lời gọi). Token hỏng → rỗng.
List<String> rolesFromJwt(String? token) {
  if (token == null) return const [];
  final parts = token.split('.');
  if (parts.length < 2) return const [];
  try {
    final body = utf8.decode(base64Url.decode(base64Url.normalize(parts[1])));
    final j = jsonDecode(body);
    if (j is! Map) return const [];
    final ra = j['realm_access'];
    if (ra is! Map) return const [];
    final roles = ra['roles'];
    if (roles is! List) return const [];
    return [for (final r in roles) '$r'];
  } catch (_) {
    return const [];
  }
}

/// Mã tài khoản (claim `sub`) trong access token — cùng quy ước không xác minh
/// chữ ký như [rolesFromJwt]. Thiếu hoặc token hỏng → null.
String? subjectFromJwt(String? token) {
  if (token == null) return null;
  final parts = token.split('.');
  if (parts.length < 2) return null;
  try {
    final body = utf8.decode(base64Url.decode(base64Url.normalize(parts[1])));
    final j = jsonDecode(body);
    if (j is! Map) return null;
    final sub = j['sub'];
    return sub is String && sub.isNotEmpty ? sub : null;
  } catch (_) {
    return null;
  }
}
