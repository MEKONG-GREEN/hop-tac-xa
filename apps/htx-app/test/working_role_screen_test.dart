import 'package:agrigreen_htx/ui/providers.dart';
import 'package:agrigreen_htx/ui/screens/home_screen.dart';
import 'package:agrigreen_htx/ui/screens/working_role_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:agrigreen_mobile/data/secure/secure_store.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
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
      // AH-270 AC-06: hai thẻ ghi tên HTX làm việc.
      expect(find.text('Chọn vai trò và HTX làm việc'), findsOneWidget);
      expect(find.text('HTX Tân Phú'), findsNWidgets(2));
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
      await tester.tap(find.byTooltip('Đổi vai trò, HTX làm việc'));
      await settle(tester);
      await tester.tap(find.text('Hủy'));
      await settle(tester);
      expect(find.byType(HomeScreen), findsOneWidget);
      await tester.tap(find.byTooltip('Đổi vai trò, HTX làm việc'));
      await settle(tester);
      await tester.tap(find.text('Tiếp tục'));
      await settle(tester);
      expect(find.byType(WorkingRoleScreen), findsOneWidget);
      // UX-TP-09 (AH-222): thẻ của vai vừa dùng có dấu.
      await settle(tester, 20);
      expect(find.text('Dùng lần trước'), findsOneWidget);
      await tester.tap(find.text('Đại diện HTX'));
      await settle(tester, 40);
      expect(find.text('AgriGreen HTX · Quản lý'), findsOneWidget);
      await controller.lock();
      await settle(tester);
      expect(find.byType(WorkingRoleScreen), findsNothing);
      expect(find.byType(MaterialApp), findsOneWidget);
    },
  );
  testWidgets(
    'FN-TK-01/EX-03, AC-06 (AH-222): vai Thành viên HTX bị ngừng trong lúc '
    'dùng → còn Đại diện HTX nên vào thẳng trang Quản lý kèm câu báo',
    (tester) async {
      final env = TestEnv(
        roles: ['coop_manager', 'htx_member'],
        nativeRealm: true,
      )..seed();
      await loginAndSetPin(tester, env);
      await tester.tap(find.text('Thành viên HTX'));
      await settle(tester, 40);
      expect(find.text('AgriGreen HTX · Thành viên HTX'), findsOneWidget);
      final services = containerOf(tester)
          .read(appControllerProvider)
          .services!;
      // Admin tắt "Kiêm Thành viên HTX": token làm mới chỉ còn Đại diện HTX.
      await env.store.write(
        SecureKeys.accessToken,
        fakeJwt(['coop_manager'], native: true),
      );
      env.server.workingRoleRevoked = true;
      await services.sync.syncOnce(force: true);
      env.server.workingRoleRevoked = false;
      await settle(tester, 40);
      expect(find.byType(WorkingRoleScreen), findsNothing);
      expect(find.text('AgriGreen HTX · Quản lý'), findsOneWidget);
      expect(services.workingRole.selected, 'coop_manager');
      expect(
        find.text(WorkingRoleRevokedException.userMessage),
        findsOneWidget,
      );
      await tester.tap(find.text('Đóng'));
      await settle(tester);
      expect(find.text(WorkingRoleRevokedException.userMessage), findsNothing);
      await containerOf(tester).read(appControllerProvider.notifier).lock();
      await settle(tester);
    },
  );
}
