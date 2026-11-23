import 'package:agrigreen_htx/ui/providers.dart';
import 'package:agrigreen_htx/ui/screens/home_screen.dart';
import 'package:agrigreen_htx/ui/screens/login_screen.dart';
import 'package:agrigreen_htx/ui/screens/pin_screen.dart';
import 'package:agrigreen_mobile/application/auth_flow.dart';
import 'package:agrigreen_mobile/data/secure/secure_store.dart';
import 'package:agrigreen_mobile/testing.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/test_env.dart';

// AH-300 (FN-01): màn đăng nhập riêng của app ở realm HTX.
void main() {
  TestEnv native() =>
      TestEnv(roles: const ['htx_member'], nativeRealm: true)..seed();

  Future<void> open(WidgetTester tester, TestEnv env) async {
    await tester.pumpWidget(appUnderTest(env));
    await settle(tester);
  }

  Future<void> submit(
    WidgetTester tester,
    String identifier,
    String password,
  ) async {
    await tester.enterText(
      find.byKey(const ValueKey('login-identifier')),
      identifier,
    );
    await tester.enterText(
      find.byKey(const ValueKey('login-password')),
      password,
    );
    await tester.ensureVisible(find.text('Đăng nhập'));
    await tester.tap(find.text('Đăng nhập'));
    await settle(tester);
  }

  AuthStage stage(WidgetTester tester) =>
      containerOf(tester).read(appControllerProvider).view.stage;

  for (final identifier in [testUsername, testPhone]) {
    testWidgets('AC-01, AC-09: đăng nhập bằng $identifier', (tester) async {
      final env = native();
      await open(tester, env);
      expect(find.byKey(const ValueKey('login-identifier')), findsOneWidget);
      await submit(tester, identifier, testPassword);
      expect(find.byType(PinScreen), findsOneWidget, reason: identifier);
      expect(stage(tester), AuthStage.needsPinSetup);
      // Token lưu như hiện nay; mật khẩu không nằm trong kho bí mật.
      expect(env.store.values[SecureKeys.tokenSource], 'password');
      expect(env.store.values.values, isNot(contains(testPassword)));
      // Không mở trang xác thực Keycloak.
      expect(env.oidc.signIns, 0);
    });
  }

  testWidgets('AC-N1: bỏ trống báo tại trường, không gửi yêu cầu', (
    tester,
  ) async {
    final env = native();
    await open(tester, env);
    await submit(tester, '', '');
    expect(find.text('Nhập tên đăng nhập hoặc số điện thoại'), findsOneWidget);
    expect(find.text('Nhập mật khẩu'), findsOneWidget);
    expect(env.password.logins, 0);
    expect(find.byType(LoginScreen), findsOneWidget);
  });

  testWidgets(
    'AC-02, AC-08, AC-N3: sai báo số lần còn lại; lần 5 khóa tạm; nhập đúng '
    'trong thời gian khóa vẫn bị chặn',
    (tester) async {
      final env = native();
      await open(tester, env);
      for (var i = 1; i <= 4; i++) {
        await submit(tester, testUsername, 'sai-$i');
        expect(
          find.text('Sai tài khoản hoặc mật khẩu. Còn ${5 - i} lần thử.'),
          findsOneWidget,
        );
      }
      await submit(tester, testUsername, 'sai-5');
      expect(find.textContaining('Thử lại sau 15 phút'), findsOneWidget);
      await submit(tester, testUsername, testPassword);
      expect(find.textContaining('Thử lại sau 15 phút'), findsOneWidget);
      expect(find.byType(LoginScreen), findsOneWidget);
      expect(stage(tester), AuthStage.needsOnlineLogin);
    },
  );

  testWidgets('AC-03: tài khoản đã khóa bị chặn; mở khóa thì vào được', (
    tester,
  ) async {
    final env = native();
    env.password.lock(testUsername);
    await open(tester, env);
    await submit(tester, testUsername, testPassword);
    expect(
      find.text(
        'Tài khoản đã bị khóa. Liên hệ người đã cấp tài khoản để mở khóa.',
      ),
      findsOneWidget,
    );
    expect(find.byType(LoginScreen), findsOneWidget);
    env.password.lock(testUsername, locked: false);
    await submit(tester, testUsername, testPassword);
    expect(find.byType(PinScreen), findsOneWidget);
  });

  testWidgets(
    'AH-269: mật khẩu tạm → đổi mật khẩu lần đầu; báo lỗi tại trường rồi vào',
    (tester) async {
      final env = native();
      env.password.addAccount(
        'moi',
        '0912000999',
        'Tam12345',
        mustChange: true,
      );
      await open(tester, env);
      await submit(tester, 'moi', 'Tam12345');
      expect(find.byKey(const ValueKey('login-new_password')), findsOneWidget);
      Future<void> change(String next, String confirm) async {
        await tester.enterText(
          find.byKey(const ValueKey('login-new_password')),
          next,
        );
        await tester.enterText(
          find.byKey(const ValueKey('login-confirm_password')),
          confirm,
        );
        await tester.ensureVisible(find.text('Đổi mật khẩu'));
        await tester.tap(find.text('Đổi mật khẩu'));
        await settle(tester);
      }

      await change('12345', '12345');
      expect(find.text('Mật khẩu tối thiểu 06 ký tự'), findsOneWidget);
      await change('matkhau2', 'matkhau3');
      expect(find.text('Mật khẩu nhập lại không khớp'), findsOneWidget);
      await change('matkhau2', 'matkhau2');
      expect(find.byType(PinScreen), findsOneWidget);
    },
  );

  testWidgets(
    'tài khoản còn việc lần đầu → báo và mở trang xác thực để hoàn tất',
    (tester) async {
      final env = native();
      env.password.requireSetup(testUsername);
      await open(tester, env);
      await submit(tester, testUsername, testPassword);
      expect(find.textContaining('hoàn tất thiết lập lần đầu'), findsOneWidget);
      await tester.ensureVisible(find.text('Hoàn tất trên trang xác thực'));
      await tester.tap(find.text('Hoàn tất trên trang xác thực'));
      await settle(tester);
      expect(env.oidc.signIns, 1);
      expect(env.store.values.containsKey(SecureKeys.tokenSource), isFalse);
    },
  );

  testWidgets('Quên mật khẩu: gửi mã qua SMS, đặt mật khẩu mới rồi đăng nhập', (
    tester,
  ) async {
    final env = native();
    await open(tester, env);
    await tester.ensureVisible(find.text('Quên mật khẩu?'));
    await tester.tap(find.text('Quên mật khẩu?'));
    await settle(tester);
    await tester.enterText(
      find.byKey(const ValueKey('forgot-identifier')),
      testPhone,
    );
    await tester.tap(find.text('Gửi mã OTP'));
    await settle(tester);
    expect(find.textContaining('0912***678'), findsOneWidget);
    await tester.enterText(
      find.byKey(const ValueKey('forgot-otp')),
      FakePasswordLoginApi.otpCode,
    );
    await tester.enterText(
      find.byKey(const ValueKey('forgot-new_password')),
      'matkhau9',
    );
    await tester.enterText(
      find.byKey(const ValueKey('forgot-confirm_password')),
      'matkhau9',
    );
    await tester.tap(find.text('Đặt mật khẩu mới'));
    await settle(tester);
    expect(find.textContaining('Đã đặt mật khẩu mới'), findsOneWidget);
    await tester.tap(find.text('Đăng nhập'));
    await settle(tester);
    await submit(tester, testPhone, 'matkhau9');
    expect(find.byType(PinScreen), findsOneWidget);
  });

  testWidgets('AC-01, AC-05: đăng nhập → PIN → trang chủ; đăng xuất về màn '
      'đăng nhập và thu hồi phiên ở máy chủ', (tester) async {
    final env = TestEnv(roles: const ['coop_manager'], nativeRealm: true)
      ..seed();
    await loginToHome(tester, env);
    expect(find.byType(HomeScreen), findsOneWidget);
    await containerOf(tester).read(appControllerProvider.notifier).signOut();
    await settle(tester, 30);
    expect(find.byType(LoginScreen), findsOneWidget);
    expect(env.password.loggedOut, ['p-rt']);
    await finish(tester);
  });
}
