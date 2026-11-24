import 'package:flutter_test/flutter_test.dart';
import 'package:agrigreen_htx/application/working_role.dart';
import 'package:agrigreen_htx/domain/roles.dart';

void main() {
  test('dual native role needs a choice; another role cannot be injected', () {
    final choice = HtxWorkingRole(
      enabled: true,
      roles: [HtxRole.coopManager, HtxRole.htxMember],
    );
    expect(choice.needsSelection, isTrue);
    choice.select(HtxRole.htxMember);
    expect(choice.needsSelection, isFalse);
    expect(choice.selected, HtxRole.htxMember);
    expect(() => choice.select(HtxRole.admin), throwsStateError);
    choice.chooseAgain();
    expect(choice.needsSelection, isTrue);
  });
  test(
    'single role skips chooser; compatibility deployment does not add a header',
    () {
      expect(
        HtxWorkingRole(enabled: true, roles: [HtxRole.coopManager]).selected,
        HtxRole.coopManager,
      );
      final legacy = HtxWorkingRole(
        enabled: false,
        roles: [HtxRole.coopManager, HtxRole.htxMember],
      );
      expect(legacy.selected, isNull);
      expect(legacy.needsSelection, isFalse);
    },
  );
  test(
    'FN-TK-01/EX-03 (AH-222): token làm mới thiếu vai đang chọn bỏ lựa chọn',
    () {
      final choice = HtxWorkingRole(
        enabled: true,
        roles: [HtxRole.coopManager, HtxRole.htxMember],
      )..select(HtxRole.htxMember);
      choice.reset([HtxRole.coopManager, HtxRole.htxMember]);
      expect(choice.selected, HtxRole.htxMember);
      // Còn một vai → dùng luôn vai đó (FN-TK-01/AC-01).
      choice.reset([HtxRole.coopManager]);
      expect(choice.available, [HtxRole.coopManager]);
      expect(choice.selected, HtxRole.coopManager);
      expect(choice.needsSelection, isFalse);
      // Vai đang chọn mất, còn hai vai khác → phải chọn lại.
      choice.reset([HtxRole.admin, HtxRole.htxMember]);
      expect(choice.selected, isNull);
      expect(choice.needsSelection, isTrue);
      // Bản tương thích (không bật chọn vai) không đổi gì.
      final legacy = HtxWorkingRole(enabled: false, roles: [HtxRole.htxMember])
        ..reset([HtxRole.coopManager]);
      expect(legacy.available, [HtxRole.htxMember]);
    },
  );
}
