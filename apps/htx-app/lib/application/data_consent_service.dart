import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/secure/secure_store.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';

import '../domain/roles.dart';

/// Kết quả kiểm tra Đồng ý thu thập dữ liệu (AH-23, REQ-011, KH-24).
class DataConsentCheck {
  const DataConsentCheck.ok() : version = null, text = null, message = null;

  /// Phải hiện màn Đồng ý với nội dung [text] của phiên bản [version].
  const DataConsentCheck.prompt({
    required String this.version,
    required String this.text,
    this.message,
  });

  /// Không xác định được trạng thái (lỗi mạng khi chưa có bản nhớ, lỗi máy
  /// chủ): vẫn chặn — chỉ cho thử lại hoặc đăng xuất.
  const DataConsentCheck.blocked(String this.message)
    : version = null,
      text = null;

  final String? version;
  final String? text;
  final String? message;

  bool get isOk => version == null && message == null;
  bool get isPrompt => version != null && text != null;
  bool get isBlocked => version == null && message != null;
}

/// Đồng ý thu thập ảnh GPS và dữ liệu sản xuất (`GET/POST /v1/me/data-consent`).
/// Hỏi ngay sau khi mở khóa, trước khi vào ứng dụng; máy chủ là nơi quyết định
/// (chặn cả route ảnh/GPS khi chưa đồng ý). Ngoại tuyến dùng bản nhớ theo
/// tài khoản; không có bản nhớ thì chặn.
class DataConsentService {
  DataConsentService({
    required SyncApi api,
    required SecureStore store,
    required IdGenerator ids,
  }) : _api = api,
       _store = store,
       _ids = ids;

  final SyncApi _api;
  final SecureStore _store;
  final IdGenerator _ids;

  static const path = '/v1/me/data-consent';
  static const cacheKey = 'htx.data_consent';
  static const staleCode = 'accounts.data_consent_version_stale';
  static const offlineMessage =
      'Cần kết nối mạng để xác nhận Đồng ý thu thập dữ liệu.';

  /// Một khóa cho mỗi lần bấm Đồng ý: các lần thử lại giữ cùng khóa.
  String? _key;

  Future<DataConsentCheck> check() async {
    try {
      final j = await _api.getJson(path);
      final version = j['version'];
      if (j['required'] != true) {
        // Tài khoản không thuộc diện đồng ý (version null) cũng được nhớ, để
        // mở khóa ngoại tuyến không bị chặn oan.
        await _remember(version is String ? version : '');
        return const DataConsentCheck.ok();
      }
      await forget();
      final text = j['text'];
      if (version is String &&
          version.isNotEmpty &&
          text is String &&
          text.isNotEmpty) {
        return DataConsentCheck.prompt(version: version, text: text);
      }
      return const DataConsentCheck.blocked(
        'Máy chủ trả nội dung Đồng ý thu thập dữ liệu không hợp lệ.',
      );
    } on NetworkException {
      return await _cachedForCurrentAccount()
          ? const DataConsentCheck.ok()
          : const DataConsentCheck.blocked(offlineMessage);
    } on ApiException catch (e) {
      return DataConsentCheck.blocked(e.detail ?? e.code);
    }
  }

  /// Ghi đồng ý phiên bản của [prompt]. Thất bại thì trả lại màn Đồng ý kèm lý
  /// do (thử lại dùng cùng khóa); nội dung vừa đổi (409) thì đọc lại bản mới.
  Future<DataConsentCheck> accept(DataConsentCheck prompt) async {
    final version = prompt.version;
    final text = prompt.text;
    if (version == null || text == null) return prompt;
    try {
      await _api.sendJson(
        method: 'POST',
        path: path,
        idempotencyKey: _key ??= _ids.newId(),
        body: {'version': version},
      );
      _key = null;
      await _remember(version);
      return const DataConsentCheck.ok();
    } on NetworkException {
      return DataConsentCheck.prompt(
        version: version,
        text: text,
        message: 'Không kết nối được máy chủ. Vui lòng thử lại.',
      );
    } on ApiException catch (e) {
      if (e.status == 409 && e.code == staleCode) {
        _key = null;
        final next = await check();
        return next.isPrompt
            ? DataConsentCheck.prompt(
                version: next.version!,
                text: next.text!,
                message:
                    'Nội dung đã được cập nhật. Vui lòng đọc lại và xác nhận.',
              )
            : next;
      }
      return DataConsentCheck.prompt(
        version: version,
        text: text,
        message: e.detail ?? e.code,
      );
    }
  }

  /// Xóa bản nhớ (không đồng ý, đăng xuất, máy chủ đòi hỏi lại).
  Future<void> forget() async {
    _key = null;
    await _store.delete(cacheKey);
  }

  Future<String?> _subject() async =>
      subjectFromJwt(await _store.read(SecureKeys.accessToken));

  Future<void> _remember(String version) async {
    final sub = await _subject();
    if (sub == null) return;
    await _store.write(cacheKey, '$sub|$version');
  }

  Future<bool> _cachedForCurrentAccount() async {
    final sub = await _subject();
    final cached = await _store.read(cacheKey);
    return sub != null && cached != null && cached.startsWith('$sub|');
  }
}
