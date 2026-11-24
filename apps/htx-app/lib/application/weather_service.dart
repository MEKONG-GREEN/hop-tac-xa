import 'dart:convert';

import 'package:agrigreen_mobile/data/db/local_db.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';

import '../domain/weather.dart';
import 'profile_service.dart';

class WeatherException implements Exception {
  WeatherException(this.message);
  final String message;
  @override
  String toString() => message;
}

/// Thời tiết HTX của mình (FN-TT-01, FN-TT-02): `GET /v1/weather/today` và
/// `GET /v1/weather/calendar`. Máy chủ xác định HTX theo tài khoản (Đại diện
/// HTX, Thành viên HTX); ngoại tuyến dùng bản đã lưu gần nhất và báo
/// "Cập nhật lúc …" (FN-TT-01/EX-01).
class WeatherService {
  WeatherService({
    required SyncApi api,
    required LocalDb db,
    required ProfileService profile,
  }) : _api = api,
       _db = db,
       _profile = profile;

  final SyncApi _api;
  final LocalDb _db;
  final ProfileService _profile;

  static const _todayKey = 'htx.weather.today';
  static const _calendarKey = 'htx.weather.calendar';

  Future<WeatherToday> today() async {
    final (j, fresh) = await _load(_todayKey, '/v1/weather/today', null);
    return WeatherToday.fromJson(j, fresh: fresh);
  }

  /// [month] dạng `YYYY-MM`.
  Future<WeatherCalendar> calendar(String month) async {
    final (j, fresh) = await _load(
      '$_calendarKey:$month',
      '/v1/weather/calendar',
      {'month': month},
    );
    return WeatherCalendar.fromJson(j, fresh: fresh);
  }

  Future<(Map<String, Object?>, bool)> _load(
    String key,
    String path,
    Map<String, String>? query,
  ) async {
    final coop = await _profile.cooperativeRef();
    if (coop == null) {
      throw WeatherException('Tài khoản chưa gắn HTX.');
    }
    final cacheKey = '$key:$coop';
    try {
      final j = await _api.getJson(path, query: query);
      await _db.setMeta(cacheKey, jsonEncode(j));
      return (j, true);
    } on NetworkException {
      final cached = await _db.getMeta(cacheKey);
      if (cached == null) {
        throw WeatherException(
          'Chưa có mạng và chưa có số liệu thời tiết đã lưu.',
        );
      }
      return ((jsonDecode(cached) as Map).cast<String, Object?>(), false);
    } on ApiException catch (e) {
      throw WeatherException(switch (e.status) {
        403 => 'Tài khoản không được xem thời tiết của HTX này.',
        _ => 'Chưa tải được thời tiết, thử lại sau.',
      });
    }
  }
}
