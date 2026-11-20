import '../domain/roles.dart';

/// A session-local authorization choice; the original JWT is never modified.
class HtxWorkingRole {
  HtxWorkingRole({required this.enabled, required Iterable<String> roles})
    : available = [
        for (final role in [
          HtxRole.admin,
          HtxRole.coopManager,
          HtxRole.htxMember,
        ])
          if (roles.contains(role)) role,
      ] {
    if (enabled && available.length == 1) selected = available.single;
  }

  final bool enabled;
  final List<String> available;
  String? selected;
  bool get needsSelection =>
      enabled && available.length > 1 && selected == null;

  void select(String role) {
    if (!enabled || !available.contains(role)) {
      throw StateError('Vai trò không thuộc tài khoản');
    }
    selected = role;
  }

  void chooseAgain() {
    if (enabled) selected = null;
  }
}
