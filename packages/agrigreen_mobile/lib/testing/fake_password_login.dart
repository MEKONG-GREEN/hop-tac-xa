import '../data/auth/oidc_client.dart';
import '../data/auth/password_login.dart';

class _FakeAccount {
  _FakeAccount(this.password, {this.mustChange = false});
  String password;
  bool mustChange;
  bool locked = false;
  bool setupRequired = false;
  int failures = 0;
}

/// API đăng nhập riêng giả, theo hành vi của máy chủ (AH-268): sai 5 lần → khóa
/// tạm 15 phút (kể cả khi sau đó nhập đúng); tên đăng nhập và số điện thoại
/// cùng trỏ một tài khoản. Không lưu mật khẩu đã gửi, chỉ đếm số lần gọi.
class FakePasswordLoginApi implements PasswordLoginApi {
  FakePasswordLoginApi({required this.issue});

  /// Token cấp khi đăng nhập/làm mới thành công.
  final OidcTokens Function() issue;

  final Map<String, _FakeAccount> _byIdentifier = {};
  int logins = 0;
  final List<String> refreshed = [];
  final List<String> loggedOut = [];
  static const maxAttempts = 5;
  static const lockMinutes = 15;

  /// Thêm tài khoản; [mustChange]: mật khẩu tạm, phải đổi lần đầu (AH-269).
  void addAccount(
    String username,
    String phone,
    String password, {
    bool mustChange = false,
  }) {
    final a = _FakeAccount(password, mustChange: mustChange);
    _byIdentifier[username] = a;
    _byIdentifier[phone] = a;
  }

  void lock(String identifier, {bool locked = true}) =>
      _byIdentifier[identifier]!.locked = locked;

  void requireSetup(String identifier) =>
      _byIdentifier[identifier]!.setupRequired = true;

  @override
  Future<LoginOutcome> login(
    String identifier,
    String password, {
    String? otp,
    String? challengeId,
    FirstPasswordChange? change,
  }) async {
    logins++;
    final a = _byIdentifier[identifier];
    if (a == null) {
      return const LoginOutcome(LoginKind.invalid, remaining: maxAttempts - 1);
    }
    if (a.failures >= maxAttempts) {
      return const LoginOutcome(LoginKind.locked, minutes: lockMinutes);
    }
    if (a.password != password) {
      a.failures++;
      return a.failures >= maxAttempts
          ? const LoginOutcome(LoginKind.locked, minutes: lockMinutes)
          : LoginOutcome(
              LoginKind.invalid,
              remaining: maxAttempts - a.failures,
            );
    }
    if (a.locked) return const LoginOutcome(LoginKind.accountLocked);
    if (a.setupRequired) return const LoginOutcome(LoginKind.setupRequired);
    a.failures = 0;
    if (a.mustChange) {
      if (change == null) {
        return const LoginOutcome(LoginKind.passwordChangeRequired);
      }
      final fields = <String, String>{
        if (change.newPassword.length < 6)
          'new_password': 'Mật khẩu tối thiểu 06 ký tự'
        else if (change.newPassword == password)
          'new_password': 'Mật khẩu mới phải khác mật khẩu tạm',
        if (change.confirmPassword != change.newPassword)
          'confirm_password': 'Mật khẩu nhập lại không khớp',
      };
      if (fields.isNotEmpty) {
        return LoginOutcome(LoginKind.passwordInvalid, fields: fields);
      }
      a
        ..password = change.newPassword
        ..mustChange = false;
    }
    return LoginOutcome(LoginKind.authenticated, tokens: issue());
  }

  @override
  Future<OidcTokens> refresh(String refreshToken) async {
    refreshed.add(refreshToken);
    return issue();
  }

  @override
  Future<void> logout(String refreshToken) async => loggedOut.add(refreshToken);

  @override
  Future<ResetOutcome> requestPasswordOtp(String identifier) async =>
      _byIdentifier.containsKey(identifier)
      ? const ResetOutcome(
          ResetKind.sent,
          otp: OtpPrompt(
            challengeId: 'c-1',
            hint: '0912***678',
            expiresIn: 300,
            resendAfter: 60,
          ),
        )
      : const ResetOutcome(
          ResetKind.failed,
          message: 'Không gửi được mã OTP cho tài khoản này.',
        );

  /// Mã OTP đúng của bản giả.
  static const otpCode = '123456';

  @override
  Future<ResetOutcome> resetPassword({
    required String identifier,
    required String challengeId,
    required String otp,
    required String newPassword,
    required String confirmPassword,
  }) async {
    if (otp != otpCode) {
      return const ResetOutcome(
        ResetKind.fieldErrors,
        fields: {'otp': 'Mã OTP không đúng'},
      );
    }
    _byIdentifier[identifier]!
      ..password = newPassword
      ..mustChange = false
      ..failures = 0;
    return const ResetOutcome(ResetKind.done);
  }
}
