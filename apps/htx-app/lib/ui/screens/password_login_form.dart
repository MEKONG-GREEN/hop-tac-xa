import 'package:agrigreen_mobile/data/auth/password_login.dart';
import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers.dart';
import 'forgot_password_screen.dart';

/// Câu thông báo của màn đăng nhập — giống web (`LoginCard`, `LoginOtpForm`,
/// `FirstPasswordForm`, trang `/login`).
abstract final class LoginMessages {
  static const identifierRequired = 'Nhập tên đăng nhập hoặc số điện thoại';
  static const passwordRequired = 'Nhập mật khẩu';
  static const accountLocked =
      'Tài khoản đã bị khóa. Liên hệ người đã cấp tài khoản để mở khóa.';
  static String tempLocked(int minutes) =>
      'Tài khoản tạm khóa đăng nhập do nhập sai nhiều lần. '
      'Thử lại sau $minutes phút hoặc dùng Quên mật khẩu?';
  static String failed(int remaining) =>
      'Sai tài khoản hoặc mật khẩu. Còn $remaining lần thử.';
  static const setupRequired =
      'Tài khoản cần hoàn tất thiết lập lần đầu (đổi mật khẩu tạm hoặc đăng ký '
      'mã OTP) trên trang xác thực.';

  /// FN-TK-01/EX-02 (AH-222), nguyên văn BRD.
  static const noActiveRole =
      'Tài khoản chưa được gán vai trò. Liên hệ quản trị viên.';

  /// FN-TK-02/AC-04 (AH-223): mật khẩu tạm quá 72 giờ.
  static const temporaryPasswordExpired =
      'Mật khẩu tạm đã quá 72 giờ nên không còn dùng được. Dùng "Quên mật '
      'khẩu?" hoặc liên hệ người đã cấp tài khoản để được cấp lại.';
  static const network =
      'Không kết nối được máy chủ đăng nhập. Vui lòng thử lại sau.';
  static const rateLimited =
      'Bạn thử đăng nhập quá nhanh. Vui lòng đợi một lát rồi thử lại.';
  static const otpEmailMissing =
      'Tài khoản chưa có email đã xác minh để nhận mã OTP. Liên hệ quản trị '
      'hệ thống.';
  static const otpUnavailable =
      'Chưa gửi được mã OTP qua email. Vui lòng thử lại sau hoặc liên hệ quản '
      'trị hệ thống.';
  static String otpRateLimited(int minutes) =>
      'Đã gửi quá nhiều mã OTP. Thử lại sau $minutes phút.';
  static const unavailable =
      'Đăng nhập tạm thời chưa dùng được. Vui lòng thử lại sau hoặc liên hệ '
      'quản trị viên.';
  static String otpSent(String hint) =>
      'Mã OTP gồm 6 chữ số đã được gửi tới '
      '${hint.isEmpty ? 'email của tài khoản' : hint}. Mã có hiệu lực 5 phút.';
  static const otpRequired = 'Nhập mã OTP gồm 6 chữ số';
  static String otpInvalid(int remaining) =>
      'Mã OTP không đúng. Còn $remaining lần thử.';
  static const otpExpired =
      'Mã OTP đã hết hạn hoặc đã bị hủy. Bấm “Gửi lại mã” để nhận mã mới.';
  static const passwordShort = 'Mật khẩu tối thiểu 06 ký tự';
  static const passwordMismatch = 'Mật khẩu nhập lại không khớp';
  static const passwordSameAsTemp = 'Mật khẩu mới phải khác mật khẩu tạm';

  /// Câu của kết quả không có cách hiển thị riêng.
  static String of(LoginOutcome r) => switch (r.kind) {
    LoginKind.invalid => failed(r.remaining),
    LoginKind.locked => tempLocked(r.minutes),
    LoginKind.accountLocked => accountLocked,
    LoginKind.setupRequired => setupRequired,
    LoginKind.noActiveRole => noActiveRole,
    LoginKind.temporaryPasswordExpired => temporaryPasswordExpired,
    LoginKind.network => network,
    LoginKind.rateLimited => rateLimited,
    LoginKind.otpEmailMissing => otpEmailMissing,
    LoginKind.otpUnavailable => otpUnavailable,
    LoginKind.otpRateLimited => otpRateLimited(r.minutes),
    LoginKind.otpInvalid => otpInvalid(r.remaining),
    LoginKind.otpExpired => otpExpired,
    _ => unavailable,
  };
}

enum _Step { credentials, otp, firstPassword }

/// AH-300 (FN-01): màn đăng nhập riêng của realm HTX — tên đăng nhập hoặc số
/// điện thoại + mật khẩu; mã OTP nếu tài khoản có yếu tố thứ hai; đổi mật khẩu
/// tạm ở lần đầu (AH-269). Không đếm lần sai ở app: số lần còn lại và thời
/// gian khóa do máy chủ trả. Mật khẩu, mã OTP chỉ ở trong ô nhập, không lưu.
class PasswordLoginForm extends ConsumerStatefulWidget {
  const PasswordLoginForm({super.key, required this.api});
  final PasswordLoginApi api;

  @override
  ConsumerState<PasswordLoginForm> createState() => _PasswordLoginFormState();
}

class _PasswordLoginFormState extends ConsumerState<PasswordLoginForm> {
  final _identifier = TextEditingController();
  final _password = TextEditingController();
  final _otp = TextEditingController();
  final _newPassword = TextEditingController();
  final _confirm = TextEditingController();
  var _step = _Step.credentials;
  OtpPrompt? _prompt;
  Map<String, String> _fields = const {};
  String? _error;
  bool _setup = false;
  bool _busy = false;

  @override
  void dispose() {
    for (final c in [_identifier, _password, _otp, _newPassword, _confirm]) {
      c.dispose();
    }
    super.dispose();
  }

  void _clearSecrets() {
    _password.clear();
    _otp.clear();
    _newPassword.clear();
    _confirm.clear();
  }

  Future<void> _run(Future<void> Function() f) async {
    if (_busy) return;
    setState(() {
      _busy = true;
      _error = null;
      _setup = false;
    });
    try {
      await f();
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _authenticated(LoginOutcome r) async {
    _clearSecrets();
    await ref
        .read(appControllerProvider.notifier)
        .completePasswordLogin(r.tokens!);
  }

  void _show(LoginOutcome r) => setState(() {
    _error = LoginMessages.of(r);
    _setup = r.kind == LoginKind.setupRequired;
  });

  Future<void> _submitCredentials() async {
    final fields = <String, String>{
      if (_identifier.text.trim().isEmpty)
        'identifier': LoginMessages.identifierRequired,
      if (_password.text.isEmpty) 'password': LoginMessages.passwordRequired,
    };
    setState(() => _fields = fields);
    if (fields.isNotEmpty) return;
    await _run(() async {
      final r = await widget.api.login(_identifier.text.trim(), _password.text);
      if (!mounted) return;
      switch (r.kind) {
        case LoginKind.authenticated:
          await _authenticated(r);
        case LoginKind.otpRequired:
          setState(() {
            _prompt = r.otp;
            _step = _Step.otp;
          });
        case LoginKind.passwordChangeRequired:
          setState(() => _step = _Step.firstPassword);
        default:
          _password.clear();
          _show(r);
      }
    });
  }

  Future<void> _resendOtp() => _run(() async {
    final r = await widget.api.login(_identifier.text.trim(), _password.text);
    if (!mounted) return;
    if (r.kind == LoginKind.otpRequired) {
      _otp.clear();
      setState(() => _prompt = r.otp);
    } else if (r.kind == LoginKind.authenticated) {
      await _authenticated(r);
    } else {
      _show(r);
    }
  });

  Future<void> _submitOtp() async {
    final code = _otp.text.trim();
    final fields = <String, String>{
      if (!RegExp(r'^[0-9]{6}$').hasMatch(code))
        'otp': LoginMessages.otpRequired,
    };
    setState(() => _fields = fields);
    if (fields.isNotEmpty) return;
    await _run(() async {
      final r = await widget.api.login(
        _identifier.text.trim(),
        _password.text,
        otp: code,
        challengeId: _prompt?.challengeId,
      );
      if (!mounted) return;
      switch (r.kind) {
        case LoginKind.authenticated:
          await _authenticated(r);
        case LoginKind.passwordChangeRequired:
          setState(() => _step = _Step.firstPassword);
        case LoginKind.otpInvalid ||
            LoginKind.otpExpired ||
            LoginKind.otpUnavailable ||
            LoginKind.otpRateLimited ||
            LoginKind.rateLimited ||
            LoginKind.network ||
            LoginKind.unavailable:
          _otp.clear();
          _show(r);
        default:
          // Sai mật khẩu, bị khóa, tài khoản không còn hợp lệ → về màn mật khẩu.
          _back();
          _show(r);
      }
    });
  }

  Future<void> _submitFirstPassword() async {
    final next = _newPassword.text;
    final fields = <String, String>{
      if (next.runes.length < 6)
        'new_password': LoginMessages.passwordShort
      else if (next == _password.text)
        'new_password': LoginMessages.passwordSameAsTemp,
      if (_confirm.text != next)
        'confirm_password': LoginMessages.passwordMismatch,
    };
    setState(() => _fields = fields);
    if (fields.isNotEmpty) return;
    await _run(() async {
      final r = await widget.api.login(
        _identifier.text.trim(),
        _password.text,
        change: FirstPasswordChange(next, _confirm.text),
      );
      if (!mounted) return;
      switch (r.kind) {
        case LoginKind.authenticated:
          await _authenticated(r);
        case LoginKind.passwordInvalid:
          setState(() => _fields = r.fields);
        case LoginKind.network ||
            LoginKind.unavailable ||
            LoginKind.rateLimited:
          _show(r);
        default:
          _back();
          _show(r);
      }
    });
  }

  void _back() => setState(() {
    _clearSecrets();
    _fields = const {};
    _prompt = null;
    _step = _Step.credentials;
  });

  Widget _field(
    String name,
    String label,
    TextEditingController c, {
    bool secret = false,
    TextInputType? keyboard,
    Iterable<String>? autofill,
  }) => Padding(
    padding: const EdgeInsets.only(bottom: 12),
    child: TextField(
      key: ValueKey('login-$name'),
      controller: c,
      obscureText: secret,
      enableSuggestions: !secret,
      autocorrect: false,
      keyboardType: keyboard,
      autofillHints: autofill,
      enabled: !_busy,
      decoration: InputDecoration(
        labelText: label,
        errorText: _fields[name],
        errorMaxLines: 3,
        border: const OutlineInputBorder(),
      ),
    ),
  );

  @override
  Widget build(BuildContext context) {
    final submit = switch (_step) {
      _Step.credentials => _submitCredentials,
      _Step.otp => _submitOtp,
      _Step.firstPassword => _submitFirstPassword,
    };
    return AutofillGroup(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          if (_step == _Step.credentials) ...[
            _field(
              'identifier',
              'Tên đăng nhập hoặc số điện thoại',
              _identifier,
              autofill: const [AutofillHints.username],
            ),
            _field(
              'password',
              'Mật khẩu',
              _password,
              secret: true,
              autofill: const [AutofillHints.password],
            ),
          ],
          if (_step == _Step.otp) ...[
            Text(
              LoginMessages.otpSent(_prompt?.hint ?? ''),
              style: const TextStyle(fontSize: 16),
            ),
            const SizedBox(height: 12),
            _field(
              'otp',
              'Mã OTP',
              _otp,
              keyboard: TextInputType.number,
              autofill: const [AutofillHints.oneTimeCode],
            ),
          ],
          if (_step == _Step.firstPassword) ...[
            const Text(
              'Đổi mật khẩu lần đầu: đặt mật khẩu mới thay cho mật khẩu tạm.',
              style: TextStyle(fontSize: 16),
            ),
            const SizedBox(height: 12),
            _field(
              'new_password',
              'Mật khẩu mới',
              _newPassword,
              secret: true,
              autofill: const [AutofillHints.newPassword],
            ),
            _field(
              'confirm_password',
              'Nhập lại mật khẩu mới',
              _confirm,
              secret: true,
              autofill: const [AutofillHints.newPassword],
            ),
          ],
          if (_error != null)
            Container(
              key: const ValueKey('login-error'),
              margin: const EdgeInsets.only(bottom: 12),
              padding: const EdgeInsets.all(14),
              decoration: BoxDecoration(
                color: AgColors.redBg,
                borderRadius: BorderRadius.circular(12),
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Text(
                    _error!,
                    style: const TextStyle(fontSize: 16, color: AgColors.red),
                  ),
                  if (_setup) ...[
                    const SizedBox(height: 8),
                    OutlinedButton(
                      onPressed: () => ref
                          .read(appControllerProvider.notifier)
                          .loginOnline(),
                      child: const Text('Hoàn tất trên trang xác thực'),
                    ),
                  ],
                ],
              ),
            ),
          FilledButton.icon(
            onPressed: _busy ? null : submit,
            icon: _busy
                ? const SizedBox(
                    width: 22,
                    height: 22,
                    child: CircularProgressIndicator(strokeWidth: 3),
                  )
                : const Icon(Icons.login),
            label: Text(switch (_step) {
              _Step.credentials => 'Đăng nhập',
              _Step.otp => 'Xác nhận',
              _Step.firstPassword => 'Đổi mật khẩu',
            }),
          ),
          if (_step == _Step.credentials)
            TextButton(
              onPressed: _busy
                  ? null
                  : () => Navigator.of(context).push(
                      MaterialPageRoute<void>(
                        builder: (_) => ForgotPasswordScreen(api: widget.api),
                      ),
                    ),
              child: const Text('Quên mật khẩu?'),
            )
          else
            Row(
              children: [
                if (_step == _Step.otp)
                  TextButton(
                    onPressed: _busy ? null : _resendOtp,
                    child: const Text('Gửi lại mã'),
                  ),
                const Spacer(),
                TextButton(
                  onPressed: _busy ? null : _back,
                  child: const Text('Quay lại'),
                ),
              ],
            ),
        ],
      ),
    );
  }
}
