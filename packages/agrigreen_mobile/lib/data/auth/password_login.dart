import 'dart:convert';

import 'package:http/http.dart' as http;

import '../../core/clock.dart';
import 'oidc_client.dart';

/// Kết quả đăng nhập bằng màn riêng (FN-01, ADR-0060) theo mã problem của máy
/// chủ — cùng cách ánh xạ với web (`apps/web-htx/src/auth/passwordLogin.ts`).
enum LoginKind {
  authenticated,

  /// AH-269: mật khẩu tạm đúng, phải đổi mật khẩu lần đầu (chưa có token).
  passwordChangeRequired,

  /// AH-269: mật khẩu mới không hợp lệ; [LoginOutcome.fields] là câu tại trường.
  passwordInvalid,

  /// ADR-0061: mật khẩu đúng, máy chủ đã gửi mã OTP; gửi lại kèm mã.
  otpRequired,
  otpInvalid,
  otpExpired,
  otpEmailMissing,
  otpUnavailable,
  otpRateLimited,

  /// accounts.login_failed (EX-02): [LoginOutcome.remaining] lần thử còn lại.
  invalid,

  /// accounts.login_temporarily_locked: [LoginOutcome.minutes] phút còn lại.
  locked,

  /// accounts.account_locked (EX-03).
  accountLocked,

  /// accounts.login_setup_required: còn việc lần đầu — hoàn tất ở trang xác thực.
  setupRequired,
  rateLimited,
  unavailable,
  network,
}

/// Mã OTP đã gửi (thời gian tính bằng giây).
class OtpPrompt {
  const OtpPrompt({
    required this.challengeId,
    required this.hint,
    required this.expiresIn,
    required this.resendAfter,
  });
  final String challengeId;

  /// Email hoặc số điện thoại đã che.
  final String hint;
  final int expiresIn;
  final int resendAfter;
}

class LoginOutcome {
  const LoginOutcome(
    this.kind, {
    this.tokens,
    this.remaining = 0,
    this.minutes = 0,
    this.fields = const {},
    this.otp,
  });
  final LoginKind kind;
  final OidcTokens? tokens;
  final int remaining;
  final int minutes;
  final Map<String, String> fields;
  final OtpPrompt? otp;
}

/// Mật khẩu mới ở bước đổi mật khẩu lần đầu (gửi kèm mật khẩu tạm).
class FirstPasswordChange {
  const FirstPasswordChange(this.newPassword, this.confirmPassword);
  final String newPassword;
  final String confirmPassword;
}

enum ResetKind { sent, done, fieldErrors, expired, failed, network }

/// Kết quả của "Quên mật khẩu" bằng mã OTP qua SMS (FN-10, AH-280).
class ResetOutcome {
  const ResetOutcome(
    this.kind, {
    this.otp,
    this.fields = const {},
    this.message,
  });
  final ResetKind kind;
  final OtpPrompt? otp;
  final Map<String, String> fields;

  /// Câu `detail` của máy chủ, nếu có.
  final String? message;
}

/// API đăng nhập riêng của realm HTX (`/v1/public/auth/*`). Mật khẩu, mã OTP
/// chỉ nằm trong thân request; không ghi log, không lưu.
abstract class PasswordLoginApi {
  Future<LoginOutcome> login(
    String identifier,
    String password, {
    String? otp,
    String? challengeId,
    FirstPasswordChange? change,
  });

  /// Làm mới token; lỗi → [OidcException] (giống client OIDC).
  Future<OidcTokens> refresh(String refreshToken);

  /// Thu hồi phiên ở máy chủ; lỗi bị bỏ qua.
  Future<void> logout(String refreshToken);

  Future<ResetOutcome> requestPasswordOtp(String identifier);

  Future<ResetOutcome> resetPassword({
    required String identifier,
    required String challengeId,
    required String otp,
    required String newPassword,
    required String confirmPassword,
  });
}

class HttpPasswordLoginApi implements PasswordLoginApi {
  HttpPasswordLoginApi({
    required String baseUrl,
    required Clock clock,
    http.Client? client,
    this.timeout = const Duration(seconds: 20),
  }) : _base = baseUrl.replaceFirst(RegExp(r'/+$'), ''),
       _clock = clock,
       _client = client ?? http.Client();

  final String _base;
  final Clock _clock;
  final http.Client _client;
  final Duration timeout;

  Future<(int, Map<String, Object?>)> _post(
    String path,
    Map<String, Object?> body,
  ) async {
    final res = await _client
        .post(
          Uri.parse('$_base$path'),
          headers: const {
            'Content-Type': 'application/json',
            'Accept': 'application/json, application/problem+json',
          },
          body: jsonEncode(body),
        )
        .timeout(timeout);
    Map<String, Object?> json = const {};
    if (res.bodyBytes.isNotEmpty) {
      try {
        final v = jsonDecode(utf8.decode(res.bodyBytes));
        if (v is Map<String, Object?>) json = v;
      } on FormatException {
        // Phản hồi lạ: coi như không có thân.
      }
    }
    return (res.statusCode, json);
  }

  static int _int(Object? v, int fallback, [int min = 0]) {
    final n = v is num ? v.toInt() : fallback;
    return n < min ? min : n;
  }

  static Map<String, String> _fields(Map<String, Object?> problem) {
    final out = <String, String>{};
    final errors = problem['errors'];
    if (errors is List<Object?>) {
      for (final e in errors) {
        if (e is Map<String, Object?> && e['field'] is String) {
          out[e['field'] as String] = e['reason'] is String
              ? e['reason'] as String
              : 'Không hợp lệ';
        }
      }
    }
    return out;
  }

  OidcTokens? _tokens(Map<String, Object?> b) {
    final at = b['access_token'];
    final rt = b['refresh_token'];
    if (at is! String || at.isEmpty || rt is! String || rt.isEmpty) {
      return null;
    }
    return OidcTokens(
      accessToken: at,
      refreshToken: rt,
      accessTokenExpiry: _clock.now().add(
        Duration(seconds: _int(b['expires_in'], 300)),
      ),
    );
  }

  static OtpPrompt? _otp(Map<String, Object?> b, String hintKey) {
    final id = b['challenge_id'];
    if (id is! String || id.isEmpty) return null;
    final hint = b[hintKey];
    return OtpPrompt(
      challengeId: id,
      hint: hint is String ? hint : '',
      expiresIn: _int(b['expires_in'], 300),
      resendAfter: _int(b['resend_after'], 60),
    );
  }

  @override
  Future<LoginOutcome> login(
    String identifier,
    String password, {
    String? otp,
    String? challengeId,
    FirstPasswordChange? change,
  }) async {
    (int, Map<String, Object?>) r;
    try {
      r = await _post('/v1/public/auth/login', {
        'identifier': identifier,
        'password': password,
        if (otp != null && otp.isNotEmpty) 'otp': otp,
        if (challengeId != null && challengeId.isNotEmpty)
          'challenge_id': challengeId,
        if (change != null) ...{
          'new_password': change.newPassword,
          'confirm_password': change.confirmPassword,
        },
      });
    } on Object {
      return const LoginOutcome(LoginKind.network);
    }
    final (status, b) = r;
    final code = b['code'];
    switch (status) {
      case 200:
        if (b['status'] == 'password_change_required') {
          return const LoginOutcome(LoginKind.passwordChangeRequired);
        }
        if (b['status'] == 'otp_required') {
          final p = _otp(b, 'email_hint');
          return p == null
              ? const LoginOutcome(LoginKind.unavailable)
              : LoginOutcome(LoginKind.otpRequired, otp: p);
        }
        final t = b['status'] == 'authenticated' ? _tokens(b) : null;
        return t == null
            ? const LoginOutcome(LoginKind.unavailable)
            : LoginOutcome(LoginKind.authenticated, tokens: t);
      case 401:
        final remaining = _int(b['remaining_attempts'], 0);
        if (code == 'accounts.login_otp_invalid') {
          return LoginOutcome(LoginKind.otpInvalid, remaining: remaining);
        }
        return code == 'accounts.login_failed'
            ? LoginOutcome(LoginKind.invalid, remaining: remaining)
            : const LoginOutcome(LoginKind.unavailable);
      case 403:
        return LoginOutcome(switch (code) {
          'accounts.account_locked' => LoginKind.accountLocked,
          'accounts.login_otp_email_missing' => LoginKind.otpEmailMissing,
          'accounts.login_setup_required' => LoginKind.setupRequired,
          _ => LoginKind.unavailable,
        });
      case 422:
        final fields = _fields(b);
        return change != null && fields.isNotEmpty
            ? LoginOutcome(LoginKind.passwordInvalid, fields: fields)
            : const LoginOutcome(LoginKind.unavailable);
      case 410:
        return LoginOutcome(
          code == 'accounts.login_otp_expired'
              ? LoginKind.otpExpired
              : LoginKind.unavailable,
        );
      case 423:
        return LoginOutcome(
          LoginKind.locked,
          minutes: _int(b['retry_after_minutes'], 15, 1),
        );
      case 429:
        return code == 'accounts.login_otp_rate_limited'
            ? LoginOutcome(
                LoginKind.otpRateLimited,
                minutes: _int(b['retry_after_minutes'], 60, 1),
              )
            : const LoginOutcome(LoginKind.rateLimited);
      case 503:
        return LoginOutcome(
          code == 'accounts.login_otp_unavailable'
              ? LoginKind.otpUnavailable
              : LoginKind.unavailable,
        );
      default:
        return const LoginOutcome(LoginKind.unavailable);
    }
  }

  @override
  Future<OidcTokens> refresh(String refreshToken) async {
    (int, Map<String, Object?>) r;
    try {
      r = await _post('/v1/public/auth/refresh', {
        'refresh_token': refreshToken,
      });
    } on Object {
      throw OidcException('Làm mới token thất bại: không kết nối được.');
    }
    final (status, b) = r;
    final t = status == 200 ? _tokens(b) : null;
    if (t != null) return t;
    throw OidcException(
      status == 401
          ? 'Phiên đăng nhập đã hết hạn. Vui lòng đăng nhập lại.'
          : 'Làm mới token thất bại.',
    );
  }

  @override
  Future<void> logout(String refreshToken) async {
    try {
      await _post('/v1/public/auth/logout', {'refresh_token': refreshToken});
    } on Object {
      // Đăng xuất ở máy luôn xóa phiên cục bộ.
    }
  }

  static String? _detail(Map<String, Object?> b) {
    final d = b['detail'];
    return d is String && d.isNotEmpty ? d : null;
  }

  @override
  Future<ResetOutcome> requestPasswordOtp(String identifier) async {
    (int, Map<String, Object?>) r;
    try {
      r = await _post('/v1/public/auth/password-otp', {
        'identifier': identifier,
      });
    } on Object {
      return const ResetOutcome(ResetKind.network);
    }
    final (status, b) = r;
    final p = status == 200 ? _otp(b, 'phone_hint') : null;
    if (p != null) return ResetOutcome(ResetKind.sent, otp: p);
    if (status == 422) {
      final fields = _fields(b);
      if (fields.isNotEmpty) {
        return ResetOutcome(ResetKind.fieldErrors, fields: fields);
      }
    }
    return ResetOutcome(ResetKind.failed, message: _detail(b));
  }

  @override
  Future<ResetOutcome> resetPassword({
    required String identifier,
    required String challengeId,
    required String otp,
    required String newPassword,
    required String confirmPassword,
  }) async {
    (int, Map<String, Object?>) r;
    try {
      r = await _post('/v1/public/auth/password-otp/reset', {
        'identifier': identifier,
        'challenge_id': challengeId,
        'otp': otp,
        'new_password': newPassword,
        'confirm_password': confirmPassword,
      });
    } on Object {
      return const ResetOutcome(ResetKind.network);
    }
    final (status, b) = r;
    if (status == 200) return const ResetOutcome(ResetKind.done);
    if (status == 422) {
      final fields = _fields(b);
      if (fields.isNotEmpty) {
        return ResetOutcome(ResetKind.fieldErrors, fields: fields);
      }
    }
    return ResetOutcome(
      status == 410 ? ResetKind.expired : ResetKind.failed,
      message: _detail(b),
    );
  }
}
