import 'package:agrigreen_mobile/data/auth/password_login.dart';
import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:flutter/material.dart';

import 'password_login_form.dart';

/// Quên mật khẩu bằng mã OTP qua SMS (FN-10, AH-280) — giống màn web
/// `/login/quen-mat-khau` của realm HTX: gửi mã tới số điện thoại của tài
/// khoản, nhập mã và mật khẩu mới. Câu lỗi nghiệp vụ lấy từ máy chủ.
class ForgotPasswordScreen extends StatefulWidget {
  const ForgotPasswordScreen({super.key, required this.api});
  final PasswordLoginApi api;

  @override
  State<ForgotPasswordScreen> createState() => _ForgotPasswordScreenState();
}

class _ForgotPasswordScreenState extends State<ForgotPasswordScreen> {
  static const _network = 'Không kết nối được máy chủ. Vui lòng thử lại sau.';
  final _identifier = TextEditingController();
  final _otp = TextEditingController();
  final _newPassword = TextEditingController();
  final _confirm = TextEditingController();
  String? _sentTo;
  OtpPrompt? _prompt;
  Map<String, String> _fields = const {};
  String? _error;
  bool _busy = false;
  bool _done = false;

  @override
  void dispose() {
    for (final c in [_identifier, _otp, _newPassword, _confirm]) {
      c.dispose();
    }
    super.dispose();
  }

  Future<void> _run(Future<void> Function() f) async {
    if (_busy) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await f();
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _send(String who) async {
    if (who.isEmpty) {
      setState(
        () => _fields = const {'identifier': LoginMessages.identifierRequired},
      );
      return;
    }
    setState(() => _fields = const {});
    await _run(() async {
      final r = await widget.api.requestPasswordOtp(who);
      if (!mounted) return;
      setState(() {
        switch (r.kind) {
          case ResetKind.sent:
            _sentTo = who;
            _prompt = r.otp;
            _otp.clear();
          case ResetKind.fieldErrors:
            _fields = r.fields;
          case ResetKind.network:
            _error = _network;
          default:
            _error = r.message ?? 'Chưa gửi được mã OTP. Vui lòng thử lại sau.';
        }
      });
    });
  }

  Future<void> _reset() async {
    final code = _otp.text.trim();
    final next = _newPassword.text;
    final fields = <String, String>{
      if (!RegExp(r'^[0-9]{6}$').hasMatch(code))
        'otp': LoginMessages.otpRequired,
      if (next.runes.length < 6) 'new_password': LoginMessages.passwordShort,
      if (_confirm.text != next)
        'confirm_password': LoginMessages.passwordMismatch,
    };
    setState(() => _fields = fields);
    if (fields.isNotEmpty) return;
    await _run(() async {
      final r = await widget.api.resetPassword(
        identifier: _sentTo!,
        challengeId: _prompt!.challengeId,
        otp: code,
        newPassword: next,
        confirmPassword: _confirm.text,
      );
      if (!mounted) return;
      setState(() {
        switch (r.kind) {
          case ResetKind.done:
            _done = true;
            _otp.clear();
            _newPassword.clear();
            _confirm.clear();
          case ResetKind.fieldErrors:
            _fields = r.fields;
          case ResetKind.network:
            _error = _network;
          default:
            if (r.kind == ResetKind.expired) _otp.clear();
            _error =
                r.message ?? 'Không đặt lại được mật khẩu. Vui lòng thử lại.';
        }
      });
    });
  }

  Widget _field(
    String name,
    String label,
    TextEditingController c, {
    bool secret = false,
    TextInputType? keyboard,
  }) => Padding(
    padding: const EdgeInsets.only(bottom: 12),
    child: TextField(
      key: ValueKey('forgot-$name'),
      controller: c,
      obscureText: secret,
      enableSuggestions: !secret,
      autocorrect: false,
      keyboardType: keyboard,
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
    final prompt = _prompt;
    return Scaffold(
      appBar: AppBar(title: const Text('Quên mật khẩu')),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(24),
          children: [
            if (_done) ...[
              const Text(
                'Đã đặt mật khẩu mới. Đăng nhập bằng mật khẩu mới.',
                style: TextStyle(fontSize: 16),
              ),
              const SizedBox(height: 16),
              FilledButton(
                onPressed: () => Navigator.of(context).pop(),
                child: const Text('Đăng nhập'),
              ),
            ] else if (prompt == null) ...[
              const Text(
                'Nhập tên đăng nhập hoặc số điện thoại. Mã OTP sẽ được gửi qua '
                'SMS tới số điện thoại của tài khoản.',
                style: TextStyle(fontSize: 16, color: AgColors.mute),
              ),
              const SizedBox(height: 16),
              _field(
                'identifier',
                'Tên đăng nhập hoặc số điện thoại',
                _identifier,
              ),
            ] else ...[
              Text(
                'Mã OTP gồm 6 chữ số đã gửi qua SMS tới ${prompt.hint}. '
                'Mã có hiệu lực 5 phút.',
                style: const TextStyle(fontSize: 16),
              ),
              const SizedBox(height: 16),
              _field('otp', 'Mã OTP', _otp, keyboard: TextInputType.number),
              _field(
                'new_password',
                'Mật khẩu mới',
                _newPassword,
                secret: true,
              ),
              _field(
                'confirm_password',
                'Nhập lại mật khẩu mới',
                _confirm,
                secret: true,
              ),
            ],
            if (_error != null)
              Container(
                margin: const EdgeInsets.only(bottom: 12),
                padding: const EdgeInsets.all(14),
                decoration: BoxDecoration(
                  color: AgColors.redBg,
                  borderRadius: BorderRadius.circular(12),
                ),
                child: Text(
                  _error!,
                  style: const TextStyle(fontSize: 16, color: AgColors.red),
                ),
              ),
            if (!_done)
              FilledButton(
                onPressed: _busy
                    ? null
                    : prompt == null
                    ? () => _send(_identifier.text.trim())
                    : _reset,
                child: Text(prompt == null ? 'Gửi mã OTP' : 'Đặt mật khẩu mới'),
              ),
            if (!_done && prompt != null)
              TextButton(
                onPressed: _busy ? null : () => _send(_sentTo!),
                child: const Text('Gửi lại mã'),
              ),
          ],
        ),
      ),
    );
  }
}
