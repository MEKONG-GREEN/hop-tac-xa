import 'dart:convert';

import 'package:agrigreen_mobile/data/db/local_db.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';

import 'profile_service.dart';

/// Thông tin HTX của người dùng (FN-03, AH-284): app chỉ xem, không sửa.
/// Số liệu lấy nguyên từ máy chủ (`GET /v1/cooperatives/{ref}`), diện tích là
/// chuỗi thập phân — app không tính lại, không dùng số thực.
class CooperativeInfo {
  const CooperativeInfo({
    required this.ref,
    required this.name,
    required this.code,
    required this.active,
    this.taxCode,
    this.place,
    this.households,
    this.cultivatedHa,
    this.livestockHa,
    this.machineCount,
    this.fresh = false,
  });

  factory CooperativeInfo.fromJson(
    Map<String, Object?> j, {
    bool fresh = false,
  }) {
    final areas = j['household_areas'];
    final a = areas is Map ? areas : const {};
    final commune = j['commune_name'] ?? j['admin_unit_code'];
    final province = j['province_name'];
    final parts = [commune, province].whereType<String>();
    final place = parts.where((s) => s.isNotEmpty).join(', ');
    return CooperativeInfo(
      ref: '${j['ref']}',
      name: '${j['name'] ?? ''}',
      code: '${j['code'] ?? ''}',
      active: j['active'] != false,
      taxCode: j['tax_code'] as String?,
      place: place.isEmpty ? null : place,
      households: a['households'] as int?,
      cultivatedHa: a['cultivated_ha'] as String?,
      livestockHa: a['livestock_ha'] as String?,
      machineCount: j['machine_count'] as int?,
      fresh: fresh,
    );
  }

  final String ref;
  final String name;
  final String code;
  final bool active;
  final String? taxCode;
  final String? place;
  final int? households;
  final String? cultivatedHa;
  final String? livestockHa;
  final int? machineCount;

  /// false: đang xem bản đệm khi ngoại tuyến.
  final bool fresh;
}

class CooperativeInfoException implements Exception {
  CooperativeInfoException(this.message);
  final String message;
  @override
  String toString() => message;
}

/// Tải thông tin HTX của mình; ngoại tuyến dùng bản đệm gần nhất.
class CooperativeInfoService {
  CooperativeInfoService({
    required SyncApi api,
    required LocalDb db,
    required ProfileService profile,
  }) : _api = api,
       _db = db,
       _profile = profile;

  final SyncApi _api;
  final LocalDb _db;
  final ProfileService _profile;

  static const _key = 'htx.cooperative_info';

  Future<CooperativeInfo> load() async {
    final coop = await _profile.cooperativeRef();
    if (coop == null) {
      throw CooperativeInfoException('Tài khoản chưa gắn HTX.');
    }
    final cacheKey = '$_key:$coop';
    try {
      final j = await _api.getJson('/v1/cooperatives/$coop');
      await _db.setMeta(cacheKey, jsonEncode(j));
      return CooperativeInfo.fromJson(j, fresh: true);
    } on NetworkException {
      final cached = await _db.getMeta(cacheKey);
      if (cached == null) {
        throw CooperativeInfoException(
          'Chưa có mạng và chưa có thông tin HTX đã lưu.',
        );
      }
      return CooperativeInfo.fromJson(
        (jsonDecode(cached) as Map).cast<String, Object?>(),
      );
    }
  }
}
