import 'package:agrigreen_htx/ui/providers.dart';
import 'package:agrigreen_htx/ui/screens/home_screen.dart';
import 'package:agrigreen_htx/ui/screens/onboarding_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/test_env.dart';

Future<String?> _flag(WidgetTester tester) => tester.runAsync<String?>(
  () =>
      containerOf(tester)
          .read(appControllerProvider)
          .services!
          .db
          .getMeta(onboardingMetaKey),
);

void main() {
  testWidgets(
    'AH-101: lần đầu hiện hướng dẫn chữ to, bỏ qua được, không hiện lại; '
    'xem lại từ nút Hướng dẫn',
    (tester) async {
      final env = TestEnv(roles: const ['htx_member'])..seed();
      env.onboarding = true;
      await loginAndSetPin(tester, env);

      // AC1: lần đầu vào trang chủ thấy bước 1 (mở vụ), chữ và nút đủ lớn.
      expect(find.byType(OnboardingScreen), findsOneWidget);
      expect(find.text('Mở vụ cho nông hộ'), findsOneWidget);
      expect(find.text('Bước 1/3'), findsOneWidget);
      final next = find.widgetWithText(FilledButton, 'Tiếp');
      expect(tester.getSize(next).height, greaterThanOrEqualTo(48));
      final body = tester.widget<Text>(
        find.textContaining('Quy trình (SOP) sẽ tự hiện'),
      );
      expect(body.style!.fontSize, greaterThanOrEqualTo(16));
      await tester.tap(next);
      await settle(tester);
      expect(find.text('Ghi nhật ký bằng một chạm'), findsOneWidget);

      // AC2: bỏ qua → về trang chủ, ghi cờ.
      await tester.tap(find.text('Bỏ qua'));
      await settle(tester);
      expect(find.byType(OnboardingScreen), findsNothing);
      expect(find.byType(HomeScreen), findsOneWidget);
      expect(await _flag(tester), 'skipped');

      // AC3: khóa rồi mở khóa (trang chủ dựng lại) — không tự hiện lại.
      await tester.tap(find.byTooltip('Khóa'));
      await settle(tester);
      for (final d in ['1', '3', '5', '7', '9', '2']) {
        await tester.tap(find.text(d));
        await tester.pump();
      }
      await settle(tester, 30);
      expect(find.byType(HomeScreen), findsOneWidget);
      expect(find.byType(OnboardingScreen), findsNothing);

      // AC2: xem lại từ nút Hướng dẫn, đi hết ba bước.
      await tester.tap(find.byTooltip('Hướng dẫn sử dụng'));
      await settle(tester);
      expect(find.byType(OnboardingScreen), findsOneWidget);
      await tester.tap(find.text('Tiếp'));
      await settle(tester);
      await tester.tap(find.text('Tiếp'));
      await settle(tester);
      expect(find.text('Khai báo sản lượng'), findsOneWidget);
      await tester.tap(find.text('Bắt đầu dùng'));
      await settle(tester);
      expect(find.byType(OnboardingScreen), findsNothing);
      expect(await _flag(tester), 'done');
      await finish(tester);
    },
  );

  testWidgets('AH-101: Đại diện HTX không tự hiện hướng dẫn của Thành viên', (
    tester,
  ) async {
    final env = TestEnv(roles: const ['coop_manager'])..seed();
    env.onboarding = true;
    await loginToHome(tester, env);
    expect(find.byType(OnboardingScreen), findsNothing);
    expect(find.byTooltip('Hướng dẫn sử dụng'), findsNothing);
    await finish(tester);
  });
}
