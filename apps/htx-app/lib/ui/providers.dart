import 'dart:async';

import 'package:agrigreen_mobile/application/auth_flow.dart';
import 'package:agrigreen_mobile/application/push_coordinator.dart';
import 'package:agrigreen_mobile/data/auth/oidc_client.dart';
import 'package:agrigreen_mobile/data/auth/pin_service.dart';
import 'package:agrigreen_mobile/data/auth/session_service.dart';
import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/data/sync/sync_service.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../application/activation_service.dart';
import '../application/app_services.dart';
import '../application/data_consent_service.dart';
import '../domain/cropcycle.dart';
import '../domain/roles.dart';

/// Ghi đè trong `main.dart` (thiết bị) hoặc trong kiểm thử widget.
final platformProvider = Provider<HtxPlatform>(
  (ref) => throw UnimplementedError('platformProvider'),
);
final oidcProvider = Provider<OidcClient>(
  (ref) => throw UnimplementedError('oidcProvider'),
);

final sessionServiceProvider = Provider<SessionService>((ref) {
  final pf = ref.watch(platformProvider);
  return SessionService(
    store: pf.store,
    clock: pf.clock,
    oidc: ref.watch(oidcProvider),
    offlineMax: pf.config.offlineSessionMax,
  );
});

final pinServiceProvider = Provider<PinService>((ref) {
  final pf = ref.watch(platformProvider);
  return PinService(
    store: pf.store,
    clock: pf.clock,
    maxAttempts: pf.config.pinMaxAttempts,
    lockout: pf.config.pinLockout,
    hardLimit: pf.config.pinHardLimit,
  );
});

/// Kích hoạt tài khoản — dùng được trước khi đăng nhập.
final activationProvider = Provider<ActivationService>((ref) {
  final pf = ref.watch(platformProvider);
  return ActivationService(oidcIssuer: pf.config.oidcIssuer, opener: pf.dialer);
});

/// Kênh đẩy sống suốt tiến trình app (kể cả khi đang khóa).
final pushCoordinatorProvider = Provider<PushCoordinator>((ref) {
  final pf = ref.watch(platformProvider);
  final c = PushCoordinator(
    messaging: pf.push,
    notifier: pf.notifier,
    isForeground: () =>
        WidgetsBinding.instance.lifecycleState == AppLifecycleState.resumed,
  );
  ref.onDispose(c.dispose);
  return c;
});

/// Đường dẫn trong app chờ mở (thông báo được chạm, kể cả trước khi mở
/// khóa). HomeScreen tiêu thụ.
class PendingDeepLink extends Notifier<String?> {
  @override
  String? build() => null;
  void set(String? link) => state = link;
  String? take() {
    final v = state;
    state = null;
    return v;
  }
}

final pendingDeepLinkProvider = NotifierProvider<PendingDeepLink, String?>(
  PendingDeepLink.new,
);

final authFlowProvider = Provider<AuthFlow>(
  (ref) => AuthFlow(
    session: ref.watch(sessionServiceProvider),
    pins: ref.watch(pinServiceProvider),
  ),
);

class AppState {
  const AppState(
    this.view, {
    this.services,
    this.busy = false,
    this.mode = HtxMode.none,
    this.consent,
  });
  final AuthView view;
  final HtxServices? services;
  final bool busy;

  /// Trang chủ theo vai (HTX US-LOGIN-01 AC-1).
  final HtxMode mode;

  /// Khác null → chưa vào được ứng dụng: màn Đồng ý thu thập dữ liệu (hoặc
  /// màn chặn khi chưa xác nhận được) — AH-23, REQ-011.
  final DataConsentCheck? consent;

  AppState copyWith({bool? busy, HtxMode? mode}) => AppState(
    view,
    services: services,
    busy: busy ?? this.busy,
    mode: mode ?? this.mode,
    consent: consent,
  );
}

class AppController extends Notifier<AppState> {
  StreamSubscription<bool>? _conn;
  StreamSubscription<SyncStatus>? _status;
  StreamSubscription<String?>? _taps;
  Timer? _timer;

  @override
  AppState build() {
    ref.onDispose(_stopAutoSync);
    ref.onDispose(() => unawaited(_taps?.cancel()));
    Future.microtask(_init);
    return const AppState(AuthView(AuthStage.loading));
  }

  AuthFlow get _flow => ref.read(authFlowProvider);
  PushCoordinator get _push => ref.read(pushCoordinatorProvider);

  Future<void> _init() async {
    final push = _push;
    _taps = push.taps.listen(
      (link) => ref
          .read(pendingDeepLinkProvider.notifier)
          .set(link ?? '/notifications'),
    );
    try {
      await push.start();
      final launched = await push.launchPayload();
      if (launched != null) {
        ref.read(pendingDeepLinkProvider.notifier).set(launched);
      }
    } catch (_) {
      // Kênh đẩy lỗi không chặn app (hộp thư vẫn đồng bộ qua pull).
    }
    await _apply(await _flow.start());
  }

  Future<void> _apply(AuthView v) async {
    if (v.stage == AuthStage.unlocked) {
      final existing = state.services;
      final services =
          existing ??
          await HtxServices.open(
            ref.read(platformProvider),
            accessToken: ref.read(sessionServiceProvider).accessToken,
            onWiped: _onWiped,
          );
      // AH-303: Admin (chỉ dùng web) và tài khoản không có vai của App HTX →
      // màn chặn; không hỏi đồng ý, không đồng bộ, không gắn kênh đẩy. Bản đệm
      // `/v1/me` cũ không giữ màn chặn khi token hiện tại đã có vai.
      final mode = await services.profile.entryMode();
      if (mode.blocked) {
        _stopAutoSync();
        state = AppState(v, services: services, mode: mode);
        return;
      }
      // AH-23: chưa đồng ý thu thập dữ liệu thì chưa bật đồng bộ, chưa vào app.
      final consent = await services.dataConsent.check();
      if (!consent.isOk) {
        _stopAutoSync();
        state = AppState(v, services: services, consent: consent);
        return;
      }
      await _enter(services, v);
    } else {
      _stopAutoSync();
      _push.detach();
      final old = state.services;
      state = AppState(v);
      await old?.close();
    }
  }

  /// Vào ứng dụng sau khi mở khóa và (nếu cần) đã đồng ý thu thập dữ liệu.
  Future<void> _enter(HtxServices services, AuthView v) async {
    final role = services.workingRole.selected;
    if (role != null) await services.bindWorkingRole(role);
    state = AppState(
      v,
      services: services,
      mode: await services.profile.mode(),
    );
    if (services.workingRole.needsSelection) return;
    _startAutoSync(services);
    await _push.attach(services.pushHost);
    unawaited(services.sync.syncOnce(force: true));
  }

  Future<void> _busy(Future<AuthView> Function() f) async {
    state = state.copyWith(busy: true);
    try {
      await _apply(await f());
    } finally {
      state = state.copyWith(busy: false);
    }
  }

  Future<void> loginOnline() => _busy(_flow.loginOnline);
  Future<void> setPin(String pin, String confirm) =>
      _busy(() => _flow.setPin(pin, confirm));
  Future<void> unlock(String pin) => _busy(() => _flow.unlock(pin));

  /// "Đăng xuất" ở màn chặn (Admin, tài khoản không có vai của App HTX).
  Future<void> signOut() => _busy(_flow.signOut);

  /// "Đồng ý" ở màn Đồng ý thu thập dữ liệu: ghi bằng chứng rồi vào ứng dụng.
  Future<void> acceptDataConsent() async {
    final s = state.services;
    final prompt = state.consent;
    if (s == null || prompt == null || !prompt.isPrompt) return;
    state = state.copyWith(busy: true);
    try {
      await _resolveConsent(s, await s.dataConsent.accept(prompt));
    } finally {
      state = state.copyWith(busy: false);
    }
  }

  /// "Thử lại" ở màn chặn: hỏi lại trạng thái đồng ý.
  Future<void> retryDataConsent() async {
    final s = state.services;
    if (s == null) return;
    state = state.copyWith(busy: true);
    try {
      await _resolveConsent(s, await s.dataConsent.check());
    } finally {
      state = state.copyWith(busy: false);
    }
  }

  /// "Không đồng ý" / "Đăng xuất": xóa bản nhớ, đăng xuất; lần đăng nhập sau
  /// hỏi lại. Dữ liệu trên máy giữ nguyên.
  Future<void> declineDataConsent() async {
    await state.services?.dataConsent.forget();
    await _busy(_flow.signOut);
  }

  Future<void> _resolveConsent(HtxServices s, DataConsentCheck c) async {
    if (c.isOk) {
      await _enter(s, state.view);
    } else {
      state = AppState(state.view, services: s, busy: state.busy, consent: c);
    }
  }

  /// Máy chủ đòi đồng ý lại (403 accounts.data_consent_required: đổi phiên bản
  /// nội dung giữa chừng): dừng đồng bộ, mở lại màn Đồng ý. Ảnh/lệnh chờ gửi
  /// vẫn nguyên ở máy.
  Future<void> _reopenConsent(HtxServices s) async {
    _stopAutoSync();
    await s.dataConsent.forget();
    final c = await s.dataConsent.check();
    if (c.isOk) {
      _startAutoSync(s);
    } else if (state.services == s) {
      state = AppState(state.view, services: s, consent: c);
    }
  }

  /// Tài khoản bị khóa trong lúc đang dùng (FN-01/EX-06, AH-301): máy chủ từ
  /// chối lượt đồng bộ bằng 403 accounts.account_locked → đăng xuất (xóa
  /// token, giữ PIN và dữ liệu chờ gửi trên máy), về màn đăng nhập kèm câu EX-03.
  Future<void> _onAccountLocked() async {
    if (state.view.stage != AuthStage.unlocked) return;
    await _flow.signOut();
    await _apply(
      const AuthView(
        AuthStage.needsOnlineLogin,
        message: AccountLockedException.userMessage,
      ),
    );
  }

  /// Khóa màn hình: đóng CSDL, về màn PIN.
  Future<void> lock() async {
    _stopAutoSync();
    _push.detach();
    await state.services?.close();
    state = const AppState(AuthView(AuthStage.locked));
  }

  /// Đọc lại vai sau lượt đồng bộ (`/v1/me` đã về).
  Future<void> selectWorkingRole(String role) async {
    final services = state.services;
    if (services == null) return;
    await services.bindWorkingRole(role);
    await _enter(services, state.view);
  }

  void chooseWorkingRoleAgain() {
    final services = state.services;
    if (services == null) return;
    _stopAutoSync();
    _push.detach();
    services.workingRole.chooseAgain();
    state = state.copyWith();
  }

  /// Đọc lại vai sau lượt đồng bộ (`/v1/me` đã về).
  Future<void> refreshMode() async {
    final s = state.services;
    if (s == null) return;
    final m = await s.profile.mode();
    if (m == state.mode) return;
    if (m.blocked) {
      _stopAutoSync();
      _push.detach();
    }
    state = state.copyWith(mode: m);
  }

  void _onWiped() {
    _stopAutoSync();
    _push.detach();
    state = const AppState(
      AuthView(
        AuthStage.needsOnlineLogin,
        message: 'Thiết bị đã bị thu hồi. Dữ liệu trên máy đã được xóa. Liên hệ quản lý HTX để được cấp lại.',
      ),
    );
  }

  void _startAutoSync(HtxServices s) {
    _stopAutoSync();
    _status = s.sync.statusStream.listen((st) {
      if (st.state == SyncState.idle) unawaited(refreshMode());
      if (st.lastError == DataConsentRequiredException.consentCode) {
        unawaited(_reopenConsent(s));
      }
      if (st.lastError == AccountLockedException.accountLockedCode) {
        unawaited(_onAccountLocked());
      }
    });
    final pf = s.platform;
    // Có mạng trở lại → đồng bộ ngay (HTX US-LOGIN-02 AC-3).
    _conn = pf.onlineChanges?.call().listen((online) {
      if (online) unawaited(s.sync.syncOnce(force: true));
    });
    if (!pf.autoSync) return;
    // Thử lại theo chu kỳ; SyncService tự tôn trọng thời điểm lùi (US-LOG-04 AC-2).
    _timer = Timer.periodic(
      const Duration(minutes: 2),
      (_) => unawaited(s.sync.syncOnce()),
    );
  }

  void _stopAutoSync() {
    unawaited(_conn?.cancel());
    unawaited(_status?.cancel());
    _conn = null;
    _status = null;
    _timer?.cancel();
    _timer = null;
  }
}

final appControllerProvider = NotifierProvider<AppController, AppState>(
  AppController.new,
);

/// Dịch vụ đã mở khóa, dùng trong callback của widget.
extension WidgetServices on WidgetRef {
  HtxServices get services {
    final s = read(appControllerProvider).services;
    if (s == null) throw StateError('Chưa mở khóa');
    return s;
  }
}

HtxServices servicesOf(Ref ref) {
  final s = ref.watch(appControllerProvider).services;
  if (s == null) throw StateError('Chưa mở khóa');
  return s;
}

/// Làm mới khi trạng thái đồng bộ đổi.
final syncStatusStreamProvider = StreamProvider<SyncStatus>((ref) {
  final s = servicesOf(ref);
  return s.sync.statusStream;
});

class HomeSummary {
  const HomeSummary({
    required this.counts,
    required this.status,
    required this.pendingFiles,
    required this.pendingActions,
    required this.plots,
    required this.openSeasons,
    this.advice,
    this.needsSupplement = 0,
    this.reviewPending = 0,
  });
  final OutboxCounts counts;
  final SyncStatus status;
  final int pendingFiles;
  final int pendingActions;
  final int plots;
  final int openSeasons;

  /// Thẻ lời khuyên theo bước SOP hiện tại; null → ẩn (HTX US-WEATHER-02 AC-2).
  final (Season, SopStep)? advice;

  /// Nhật ký bị yêu cầu bổ sung (nông dân).
  final int needsSupplement;

  /// Nhật ký chờ duyệt (quản lý).
  final int reviewPending;

  int get waiting => counts.pending + pendingFiles + pendingActions;
}

final homeSummaryProvider = FutureProvider<HomeSummary>((ref) async {
  ref.watch(syncStatusStreamProvider);
  final s = servicesOf(ref);
  final mode = ref.watch(appControllerProvider.select((a) => a.mode));
  final seasons = await s.crop.seasons();
  return HomeSummary(
    counts: await s.outbox.counts(),
    status: await s.sync.status(),
    pendingFiles: (await s.evidence.pending()).length,
    pendingActions: await s.actions.pendingCount(),
    plots: (await s.crop.plots()).length,
    openSeasons: seasons.where((x) => x.isOpen).length,
    advice: await s.crop.advice(),
    needsSupplement: mode == HtxMode.farmer
        ? (await s.crop.needingSupplement()).length
        : 0,
    reviewPending: mode == HtxMode.manager
        ? (await s.manager.reviewQueue())
              .where((r) => r.decision == null)
              .length
        : 0,
  );
});
