import 'dart:convert';

import 'package:agrigreen_mobile/application/auth_flow.dart';
import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/data/auth/oidc_client.dart';
import 'package:agrigreen_mobile/data/auth/password_login.dart';
import 'package:agrigreen_mobile/data/auth/session_service.dart';
import 'package:agrigreen_mobile/data/secure/secure_store.dart';
import 'package:agrigreen_mobile/testing.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

// AH-300 (FN-01, ADR-0060): client đăng nhập riêng của app — cùng cách ánh xạ
// mã problem với web; thân chỉ gồm identifier/password/otp, không Authorization.
void main() {
  final clock = FakeClock(DateTime.utc(2026, 12, 1));
  final sent = <http.Request>[];

  HttpPasswordLoginApi api(int status, Map<String, Object?> body) {
    sent.clear();
    return HttpPasswordLoginApi(
      baseUrl: 'https://api.test/',
      clock: clock,
      client: MockClient((req) async {
        sent.add(req);
        return http.Response(
          jsonEncode(body),
          status,
          headers: {'content-type': 'application/json'},
        );
      }),
    );
  }

  const tokens = {
    'status': 'authenticated',
    'access_token': 'at',
    'refresh_token': 'rt',
    'expires_in': 300,
    'refresh_expires_in': 36000,
  };

  test(
    'gửi đúng đường dẫn và thân; 200 → token có hạn theo expires_in',
    () async {
      final r = await api(200, tokens).login('0912345678', 'Mat-khau-12!');
      expect(
        sent.single.url.toString(),
        'https://api.test/v1/public/auth/login',
      );
      expect(sent.single.headers['Authorization'], isNull);
      expect(jsonDecode(sent.single.body), {
        'identifier': '0912345678',
        'password': 'Mat-khau-12!',
      });
      expect(r.kind, LoginKind.authenticated);
      expect(r.tokens!.accessToken, 'at');
      expect(r.tokens!.refreshToken, 'rt');
      expect(
        r.tokens!.accessTokenExpiry,
        clock.now().add(const Duration(seconds: 300)),
      );
    },
  );

  test('ánh xạ mã problem như web', () async {
    final cases = <(int, Map<String, Object?>, LoginKind, int, int)>[
      (
        401,
        {'code': 'accounts.login_failed', 'remaining_attempts': 1},
        LoginKind.invalid,
        1,
        0,
      ),
      (
        423,
        {
          'code': 'accounts.login_temporarily_locked',
          'retry_after_minutes': 15,
        },
        LoginKind.locked,
        0,
        15,
      ),
      (403, {'code': 'accounts.account_locked'}, LoginKind.accountLocked, 0, 0),
      (
        403,
        {'code': 'accounts.login_setup_required'},
        LoginKind.setupRequired,
        0,
        0,
      ),
      (403, {'code': 'accounts.no_active_role'}, LoginKind.noActiveRole, 0, 0),
      (
        403,
        {'code': 'accounts.temporary_password_expired'},
        LoginKind.temporaryPasswordExpired,
        0,
        0,
      ),
      (
        401,
        {'code': 'accounts.login_otp_invalid', 'remaining_attempts': 3},
        LoginKind.otpInvalid,
        3,
        0,
      ),
      (410, {'code': 'accounts.login_otp_expired'}, LoginKind.otpExpired, 0, 0),
      (
        429,
        {'code': 'accounts.login_otp_rate_limited', 'retry_after_minutes': 42},
        LoginKind.otpRateLimited,
        0,
        42,
      ),
      (429, {'code': 'rate_limited'}, LoginKind.rateLimited, 0, 0),
      (
        503,
        {'code': 'accounts.login_unavailable'},
        LoginKind.unavailable,
        0,
        0,
      ),
      (401, {'code': 'unauthenticated'}, LoginKind.unavailable, 0, 0),
      (200, {'status': 'authenticated'}, LoginKind.unavailable, 0, 0),
      (
        200,
        {'status': 'password_change_required'},
        LoginKind.passwordChangeRequired,
        0,
        0,
      ),
    ];
    for (final (status, body, kind, remaining, minutes) in cases) {
      final r = await api(status, body).login('a', 'b');
      expect(
        (r.kind, r.remaining, r.minutes),
        (kind, remaining, minutes),
        reason: '$status $body',
      );
    }
  });

  test(
    'bước OTP gửi challenge_id và mã; đổi mật khẩu lần đầu gửi mật khẩu mới',
    () async {
      final step1 = await api(200, {
        'status': 'otp_required',
        'challenge_id': 'c-1',
        'email_hint': 'a***@x.vn',
        'expires_in': 300,
        'resend_after': 60,
      }).login('admin', 'x');
      expect(step1.kind, LoginKind.otpRequired);
      expect(step1.otp!.challengeId, 'c-1');
      expect(step1.otp!.hint, 'a***@x.vn');

      await api(
        200,
        tokens,
      ).login('admin', 'x', otp: '123456', challengeId: 'c-1');
      expect(jsonDecode(sent.single.body), {
        'identifier': 'admin',
        'password': 'x',
        'otp': '123456',
        'challenge_id': 'c-1',
      });

      final r =
          await api(422, {
            'code': 'validation_failed',
            'errors': [
              {
                'field': 'new_password',
                'reason': 'Mật khẩu tối thiểu 06 ký tự',
              },
            ],
          }).login(
            'moi',
            'Tam12345',
            change: const FirstPasswordChange('12345', '12345'),
          );
      expect(jsonDecode(sent.single.body), {
        'identifier': 'moi',
        'password': 'Tam12345',
        'new_password': '12345',
        'confirm_password': '12345',
      });
      expect(r.kind, LoginKind.passwordInvalid);
      expect(r.fields, {'new_password': 'Mật khẩu tối thiểu 06 ký tự'});
    },
  );

  test('không tới được máy chủ → network', () async {
    final r = await HttpPasswordLoginApi(
      baseUrl: 'https://api.test',
      clock: clock,
      client: MockClient((_) async => throw http.ClientException('offline')),
    ).login('a', 'b');
    expect(r.kind, LoginKind.network);
  });

  test(
    'refresh: 200 → token; 401 → OidcException; logout bỏ qua lỗi',
    () async {
      final t = await api(200, tokens).refresh('rt');
      expect(jsonDecode(sent.single.body), {'refresh_token': 'rt'});
      expect(t.accessToken, 'at');
      await expectLater(
        api(401, {'code': 'accounts.session_expired'}).refresh('rt'),
        throwsA(isA<OidcException>()),
      );
      await api(503, {}).logout('rt');
      expect(sent.single.url.path, '/v1/public/auth/logout');
    },
  );

  test(
    'quên mật khẩu: gửi mã → phone_hint; đặt lại → done; 410 → expired',
    () async {
      final s = await api(200, {
        'status': 'otp_sent',
        'challenge_id': 'c-9',
        'phone_hint': '0912***678',
        'expires_in': 300,
        'resend_after': 60,
      }).requestPasswordOtp('tanphu');
      expect((s.kind, s.otp!.hint), (ResetKind.sent, '0912***678'));
      final ok = await api(200, {'status': 'password_reset'}).resetPassword(
        identifier: 'tanphu',
        challengeId: 'c-9',
        otp: '123456',
        newPassword: 'matkhau1',
        confirmPassword: 'matkhau1',
      );
      expect(ok.kind, ResetKind.done);
      final gone = await api(410, {'detail': 'Mã OTP đã hết hạn'})
          .resetPassword(
            identifier: 'tanphu',
            challengeId: 'c-9',
            otp: '123456',
            newPassword: 'matkhau1',
            confirmPassword: 'matkhau1',
          );
      expect(
        (gone.kind, gone.message),
        (ResetKind.expired, 'Mã OTP đã hết hạn'),
      );
    },
  );

  group('phiên của API đăng nhập riêng', () {
    late MemorySecureStore store;
    late FakePasswordLoginApi password;
    late SessionService session;
    late FakeOidc oidc;
    setUp(() {
      store = MemorySecureStore();
      oidc = FakeOidc();
      password = FakePasswordLoginApi(
        issue: () => OidcTokens(
          accessToken: 'p-at',
          refreshToken: 'p-rt',
          accessTokenExpiry: clock.now().add(const Duration(minutes: 5)),
        ),
      );
      session = SessionService(
        store: store,
        clock: clock,
        oidc: oidc,
        passwordLogin: password,
      );
    });

    test('lưu token như hiện nay, làm mới và đăng xuất qua API', () async {
      final v = await AuthFlow(session: session).loginWithPasswordTokens(
        OidcTokens(
          accessToken: 'p-at',
          refreshToken: 'p-rt',
          accessTokenExpiry: clock.now(),
        ),
      );
      expect(v.stage, AuthStage.unlocked);
      expect(store.values[SecureKeys.accessToken], 'p-at');
      expect(await session.offlineState(), OfflineSessionState.valid);
      expect(await session.accessToken(), 'p-at');
      expect(password.refreshed, ['p-rt']);
      await session.signOut();
      expect(password.loggedOut, ['p-rt']);
      expect(store.values.containsKey(SecureKeys.tokenSource), isFalse);
      expect(await session.offlineState(), OfflineSessionState.never);
    });

    test('phiên OIDC (trang xác thực) vẫn làm mới qua OIDC', () async {
      await session.signInOnline();
      await session.refreshAccessToken();
      expect(password.refreshed, isEmpty);
      await session.signOut();
      expect(password.loggedOut, isEmpty);
    });
  });
}
