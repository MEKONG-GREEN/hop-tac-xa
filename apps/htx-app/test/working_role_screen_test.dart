import 'package:agrigreen_htx/ui/providers.dart';
import 'package:agrigreen_htx/ui/screens/home_screen.dart';
import 'package:agrigreen_htx/ui/screens/working_role_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';

import 'support/test_env.dart';

void main() {
  testWidgets('pending immutable operations keep their original working role', (
    tester,
  ) async {
    final env = TestEnv(
      roles: ['coop_manager', 'htx_member'],
      nativeRealm: true,
    )..seed();
    await loginAndSetPin(tester, env);
    await tester.tap(find.text('Thành viên HTX'));
    await settle(tester, 40);
    final services = containerOf(tester).read(appControllerProvider).services!;
    final operation = await services.outbox.enqueue(
      entity: PushEntity.fieldReport,
      payload: {'note': 'Chờ đồng bộ'},
    );
    services.workingRole.chooseAgain();
    await expectLater(
      services.bindWorkingRole('coop_manager'),
      throwsStateError,
    );
    expect(services.workingRole.selected, isNull);
    expect(
      (await services.outbox.byId(operation.id))!.idemKey,
      operation.idemKey,
    );
    expect((await services.outbox.counts()).pending, 1);
    await services.bindWorkingRole('htx_member');
    expect(services.workingRole.selected, 'htx_member');
    await containerOf(tester).read(appControllerProvider.notifier).lock();
    await settle(tester);
  });
  testWidgets(
    'native dual role selects Member and can cancel or confirm a switch',
    (tester) async {
      final env = TestEnv(
        roles: ['coop_manager', 'htx_member'],
        nativeRealm: true,
      )..seed();
      await loginAndSetPin(tester, env);
      expect(find.byType(WorkingRoleScreen), findsOneWidget);
      expect(find.byType(HomeScreen), findsNothing);
      await tester.tap(find.text('Thành viên HTX'));
      await settle(tester, 40);
      expect(find.text('AgriGreen HTX · Thành viên HTX'), findsOneWidget);
      final controller = containerOf(tester)
          .read(appControllerProvider.notifier);
      expect(
        containerOf(tester)
            .read(appControllerProvider)
            .services!
            .workingRole
            .selected,
        'htx_member',
      );
      await tester.tap(find.byTooltip('Đổi vai trò'));
      await settle(tester);
      await tester.tap(find.text('Hủy'));
      await settle(tester);
      expect(find.byType(HomeScreen), findsOneWidget);
      await tester.tap(find.byTooltip('Đổi vai trò'));
      await settle(tester);
      await tester.tap(find.text('Tiếp tục'));
      await settle(tester);
      expect(find.byType(WorkingRoleScreen), findsOneWidget);
      await tester.tap(find.text('Đại diện HTX'));
      await settle(tester, 40);
      expect(find.text('AgriGreen HTX · Quản lý'), findsOneWidget);
      await controller.lock();
      await settle(tester);
      expect(find.byType(WorkingRoleScreen), findsNothing);
      expect(find.byType(MaterialApp), findsOneWidget);
    },
  );
}
