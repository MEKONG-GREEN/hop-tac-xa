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
}
