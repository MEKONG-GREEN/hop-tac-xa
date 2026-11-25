import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';

import 'dart:convert';

import 'package:agrigreen_mobile/data/db/local_db.dart';
import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/data/reference/reference_repository.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';

import '../domain/season_plan.dart';

typedef HouseholdJson = Map<String, Object?>;

/// A copied suggestion, never a live constraint on the season being opened.
class SeasonCalendarPrefill {
  const SeasonCalendarPrefill({
    required this.cropCode,
    required this.seasonCode,
    required this.year,
    required this.anchorDate,
  });
  final String cropCode, seasonCode, anchorDate;
  final int year;
  factory SeasonCalendarPrefill.fromJson(HouseholdJson value) {
    final day = value['sow_from'];
    final year = value['season_year'];
    if (day is! String ||
        !RegExp(r'^\d{4}-\d{2}-\d{2}$').hasMatch(day) ||
        DateTime.tryParse(day)?.toIso8601String().substring(0, 10) != day ||
        year is! int ||
        year < 2000 ||
        year > 2100 ||
        value['crop_code'] is! String ||
        value['season_code'] is! String) {
      throw const FormatException('Lịch thời vụ không hợp lệ.');
    }
    return SeasonCalendarPrefill(
      cropCode: value['crop_code']! as String,
      seasonCode: value['season_code']! as String,
      year: year,
      anchorDate: day,
    );
  }
  String get displayAnchor {
    final parts = anchorDate.split('-');
    return '${parts[2]}/${parts[1]}/${parts[0]}';
  }
}

class HouseholdSopChoice {
  const HouseholdSopChoice(this.id, this.name, this.anchor);
  final String id;
  final String name;
  final String anchor;
}

String? householdAreaError(String raw) {
  if (!RegExp(r'^(0|[1-9][0-9]{0,9})(\.[0-9]{1,2})?$').hasMatch(raw) ||
      !RegExp('[1-9]').hasMatch(raw)) {
    return 'Diện tích phải lớn hơn 0, tối đa hai chữ số thập phân.';
  }
  return null;
}

/// AH-87 (KH-26): số tấn người dùng nhập — không âm, tối đa 2 chữ số thập phân.
/// Ô trống hợp lệ (dùng giá trị hệ thống tính). Máy chủ kiểm lại.
String? tonnesError(String raw) {
  final v = raw.trim().replaceAll(',', '.');
  if (v.isEmpty) return null;
  if (!RegExp(r'^[0-9]{1,30}(\.[0-9]{1,2})?$').hasMatch(v)) {
    return 'Nhập số tấn không âm, tối đa 2 chữ số thập phân.';
  }
  return null;
}

/// Chuẩn hóa ô tấn trước khi gửi: trống → null, dấu phẩy → dấu chấm.
String? tonnesValue(String raw) {
  final v = raw.trim().replaceAll(',', '.');
  return v.isEmpty ? null : v;
}

/// Season commands remain online-only. Detailed journals use the native
/// outbox capability; cached checklists live in SQLCipher and are actor scoped.
class HouseholdSeasonService {
  const HouseholdSeasonService({
    required this.api,
    required this.location,
    this.db,
    this.outbox,
    this.refs,
    this.journalContext,
  });
  final SyncApi api;
  final LocationSource location;
  final LocalDb? db;
  final OutboxRepository? outbox;
  final ReferenceRepository? refs;
  final Future<HouseholdJson> Function()? journalContext;

  Future<String?> _cacheKey(String path) async {
    HouseholdJson? context;
    try {
      context = await journalContext?.call();
    } on StateError {
      return null;
    }
    return context == null
        ? null
        : 'native.journal.${context['tenant_id']}.${context['recorded_by']}.${context['org_ref']}.$path';
  }

  Future<HouseholdJson> _cachedGet(String path) async {
    final key = await _cacheKey(path);
    try {
      final value = await api.getJson(path);
      if (key != null) await db?.setMeta(key, jsonEncode(value));
      return value;
    } catch (e) {
      if (e is ApiException && e.status < 500) {
        if (key != null) await db?.setMeta(key, null);
        rethrow;
      }
      final raw = key == null ? null : await db?.getMeta(key);
      if (raw == null) rethrow;
      return {
        ...(jsonDecode(raw) as Map).cast<String, Object?>(),
        'offline_cache': true,
      };
    }
  }

  Future<void> preparePhotoKey(String household) async {
    final repository = refs;
    if (repository == null) throw StateError('Chưa nối kho khóa ảnh.');
    final value = await _cachedGet('/v1/households/$household/photo-key');
    final key = decodePublicKey('${value['public_key']}');
    if (key == null) throw StateError('Khóa ảnh không hợp lệ.');
    await repository.cachePhotoKey(household, key);
  }

  Future<OutboxRecord> recordTask(
    String coop,
    String household,
    String season,
    HouseholdJson body,
  ) async {
    final queue = outbox;
    final identity = await journalContext?.call();
    if (queue == null || identity == null) {
      throw StateError('Chưa nối hàng đợi nhật ký.');
    }
    if (identity['org_ref'] != coop) {
      throw StateError('Nhật ký không thuộc HTX hiện tại.');
    }
    return queue.enqueue(
      entity: PushEntity.householdTaskRecord,
      payload: {
        ...body,
        'recorded_by': identity['recorded_by'],
        'tenant_id': identity['tenant_id'],
        'cooperative_ref': coop,
        'household_ref': household,
        'season_ref': season,
      },
    );
  }

  /// Có hàng đợi nhật ký trên máy (đủ để lưu khi ngoại tuyến).
  bool get canQueueOffline => outbox != null && journalContext != null;

  /// Việc hiệu lực trong bản chụp SOP của vụ (AH-113): việc tự thêm, hoặc việc gốc
  /// đã áp sửa ngày/vật tư của lớp tùy chỉnh.
  static HouseholdJson? effectiveStep(Object? snapshot, Object? seq) {
    if (snapshot is! Map) return null;
    final custom = snapshot['custom'] is Map
        ? snapshot['custom'] as Map
        : const <Object?, Object?>{};
    HouseholdJson? find(Object? list) => (list is List ? list : const [])
        .whereType<Map<Object?, Object?>>()
        .where((s) => s['seq'] == seq)
        .map((s) => s.cast<String, Object?>())
        .firstOrNull;
    final added = find(custom['added']);
    if (added != null) return added;
    final step = find(snapshot['steps']);
    if (step == null) return null;
    final edit = find(custom['edits']);
    return edit == null
        ? step
        : {...step, 'window': edit['window'], 'materials': edit['materials']};
  }

  /// AH-114 AC1: tích nhanh khi mất mạng — lưu trên máy như bản ghi việc "Hoàn
  /// tất" ngày hôm nay với vật tư khuyến cáo của việc; đồng bộ khi có mạng.
  /// Khóa idempotency là khóa của bản ghi trên máy nên gửi lại không tạo trùng.
  Future<OutboxRecord> queueTick(
    String coop,
    String household,
    HouseholdJson season,
    int seq,
    String event,
    HouseholdJson gps, {
    DateTime? now,
  }) {
    final step = effectiveStep(season['sop_snapshot'], seq);
    final day = now ?? DateTime.now();
    final materials = ((step?['materials'] as List?) ?? const [])
        .whereType<Map<Object?, Object?>>()
        .map(
          (m) => <String, Object?>{
            'material_code': '${m['material_code'] ?? ''}',
            'name': '${m['name'] ?? ''}',
            'unit': '${m['unit'] ?? ''}',
            'dose_per_ha': '${m['dose_per_ha'] ?? '0'}',
          },
        )
        .toList();
    return recordTask(coop, household, '${season['season_ref']}', {
      'expected_season_event_id': event,
      'step_seq': seq,
      'state': 'done',
      'done_on':
          '${day.year.toString().padLeft(4, '0')}-${day.month.toString().padLeft(2, '0')}-${day.day.toString().padLeft(2, '0')}',
      'materials': materials,
      'file_refs': <String>[],
      'note': '',
      'backfill_reason': '',
      'zero_confirmed': false,
      'gps': gps,
    });
  }

  /// AH-114 AC4: xóa bản ghi bị máy chủ từ chối (vd vụ đã đóng) theo ý người dùng.
  Future<bool> discardTask(String id) async =>
      (await outbox?.discardRejected(id)) ?? false;

  Future<List<OutboxRecord>> pendingTasks(
    String coop,
    String household,
    String season,
  ) async {
    HouseholdJson? identity;
    try {
      identity = await journalContext?.call();
    } on StateError {
      return [];
    }
    final current = identity;
    if (current == null || outbox == null) return [];
    if (current['org_ref'] != coop) return [];
    return (await outbox!.list(entity: PushEntity.householdTaskRecord))
        .where(
          (r) =>
              r.payload['recorded_by'] == current['recorded_by'] &&
              r.payload['tenant_id'] == current['tenant_id'] &&
              r.payload['cooperative_ref'] == coop &&
              r.payload['household_ref'] == household &&
              r.payload['season_ref'] == season &&
              r.status != OutboxStatus.sent &&
              r.status != OutboxStatus.superseded,
        )
        .toList();
  }

  Future<List<HouseholdJson>> calendars(String coop) => _pages(
    '/v1/cooperatives/${Uri.encodeComponent(coop)}/season-calendars',
    pageSize: 100,
    cursorParameter: 'after',
  );
  Future<HouseholdJson> calendar(String calendar) =>
      api.getJson('/v1/cropcycle/calendars/${Uri.encodeComponent(calendar)}');

  Future<HouseholdJson> checklist(
    String coop,
    String household,
    String season,
  ) async {
    final value = await _cachedGet(
      '/v1/cooperatives/$coop/households/$household/seasons/$season/checklist',
    );
    if (refs != null && value['offline_cache'] != true) {
      try {
        await preparePhotoKey(household);
      } catch (_) {
        // A missing encryption dependency blocks camera capture, not the
        // journal itself. PhotoCapture reports it before opening the camera.
      }
    }
    return value;
  }

  Future<HouseholdJson> completeTask(
    String coop,
    String household,
    String season,
    HouseholdJson body,
    String key,
  ) => api.sendJson(
    method: 'POST',
    path:
        '/v1/cooperatives/$coop/households/$household/seasons/$season/task-completions',
    idempotencyKey: key,
    body: {...body, 'channel': 'app'},
  );

  /// AH-126: xóa mềm nhật ký việc hoặc bỏ tích việc đã xong, kèm lý do (≥ 10 ký tự).
  /// Cần mạng; [key] giữ nguyên khi thử lại cùng thao tác.
  Future<HouseholdJson> voidTask(
    String coop,
    String household,
    String season,
    String logRef,
    String expectedId,
    String reason,
    String key,
  ) => api.sendJson(
    method: 'POST',
    path:
        '/v1/cooperatives/$coop/households/$household/seasons/$season/task-records/$logRef/void',
    idempotencyKey: key,
    body: {
      'expected_record_id': expectedId,
      'reason': reason,
      'channel': 'app',
    },
  );

  /// AH-126: các phiên bản của nhật ký việc (cũ nhất trước).
  Future<List<HouseholdJson>> taskHistory(
    String coop,
    String household,
    String season,
    String logRef,
  ) async {
    final value = await api.getJson(
      '/v1/cooperatives/$coop/households/$household/seasons/$season/task-records/$logRef/history',
    );
    return ((value['items'] as List?) ?? const [])
        .whereType<Map<Object?, Object?>>()
        .map((m) => m.cast<String, Object?>())
        .toList();
  }

  Future<SeasonPlan> previewPlan(
    String coop,
    String household,
    HouseholdJson body,
    String key,
  ) async {
    final result = await api.sendJson(
      method: 'POST',
      path: '/v1/cooperatives/$coop/households/$household/season-plan-preview',
      idempotencyKey: key,
      body: body,
    );
    return SeasonPlan.fromJson(result);
  }

  Future<SeasonPlan> plan(String coop, String household, String season) async =>
      SeasonPlan.fromJson(
        await api.getJson(
          '/v1/cooperatives/$coop/households/$household/seasons/$season/plan',
        ),
      );

  Future<List<HouseholdJson>> _pages(
    String path, {
    Map<String, String> query = const {},
    int pageSize = 200,
    String cursorParameter = 'cursor',
  }) async {
    final items = <HouseholdJson>[];
    final seen = <String>{};
    String? cursor;
    do {
      final result = await api.getJson(
        path,
        query: {...query, 'limit': '$pageSize', cursorParameter: ?cursor},
      );
      for (final row in (result['items'] as List? ?? const [])) {
        if (row is Map) items.add(row.cast<String, Object?>());
      }
      cursor = result['next_cursor'] as String?;
      if (cursor != null && !seen.add(cursor)) {
        throw StateError('Máy chủ trả phân trang lặp.');
      }
    } while (cursor != null && cursor.isNotEmpty);
    return items;
  }

  Future<List<HouseholdJson>> catalog(String catalog) =>
      _cachedPages('/v1/masterdata/catalogs/$catalog');
  Future<List<HouseholdJson>> seasons(String coop, String household) =>
      _cachedPages('/v1/cooperatives/$coop/households/$household/seasons');

  Future<List<HouseholdJson>> _cachedPages(String path) async {
    final key = await _cacheKey('pages:$path');
    try {
      final items = await _pages(path);
      if (key != null) await db?.setMeta(key, jsonEncode(items));
      return items;
    } catch (e) {
      if (e is! NetworkException) rethrow;
      final raw = key == null ? null : await db?.getMeta(key);
      if (raw == null) rethrow;
      return (jsonDecode(raw) as List)
          .map((v) => (v as Map).cast<String, Object?>())
          .toList();
    }
  }

  Future<List<HouseholdSopChoice>> sops(
    String coop,
    String crop,
    String variety,
  ) async {
    final templates = await _pages(
      '/v1/cropcycle/sops',
      query: {'status': 'published'},
    );
    final bindings = await api.getJson('/v1/cropcycle/variety-bindings');
    final binding = (bindings['items'] as List? ?? const [])
        .whereType<Map<Object?, Object?>>()
        .where((b) => b['variety_code'] == variety)
        .firstOrNull;
    final eligible = templates
        .where(
          (t) =>
              t['crop'] == crop &&
              (t['variety_code'] == null ||
                  t['variety_code'] == '' ||
                  t['variety_code'] == variety) &&
              (t['scope'] == 'system' || t['cooperative_ref'] == coop),
        )
        .toList();
    final specific = eligible
        .where(
          (t) =>
              t['variety_code'] == variety ||
              t['ref'] == binding?['template_ref'],
        )
        .toList();
    final out = <HouseholdSopChoice>[];
    for (final t in specific.isNotEmpty ? specific : eligible) {
      final detail = await api.getJson('/v1/cropcycle/sops/${t['ref']}');
      final version = (detail['versions'] as List? ?? const [])
          .whereType<Map<Object?, Object?>>()
          .where(
            (v) =>
                v['state'] == 'active' &&
                v['version_no'] == t['published_version'],
          )
          .firstOrNull;
      if (version != null) {
        out.add(
          HouseholdSopChoice(
            '${version['id']}',
            '${t['name']} · bản ${version['version_no']}',
            '${t['anchor_name']}',
          ),
        );
      }
    }
    return out;
  }

  Future<HouseholdJson> gps() async {
    try {
      final point = await location.current();
      final fix = point.fix;
      if (fix == null) {
        return {
          'source': point.status == GpsStatus.denied ? 'denied' : 'no_device',
        };
      }
      return {
        'lon': fix.lon.toStringAsFixed(7),
        'lat': fix.lat.toStringAsFixed(7),
        'accuracy_m': ?fix.accuracyM?.toStringAsFixed(2),
        'source': 'device',
      };
    } catch (_) {
      return {'source': 'no_device'};
    }
  }

  /// AH-87: Thành viên HTX nhập, sửa hoặc xóa (null) sản lượng lúa, rơm dự kiến của vụ
  /// đang canh tác. Cần mạng; [key] giữ nguyên khi thử lại cùng nội dung.
  Future<HouseholdJson> saveYieldEstimate(
    String coop,
    String household,
    String season, {
    required int expectedSeq,
    required String? riceT,
    required String? strawT,
    required String key,
  }) async {
    for (final v in [riceT, strawT]) {
      final error = v == null ? null : tonnesError(v);
      if (error != null) throw ArgumentError(error);
    }
    final result = await api.sendJson(
      method: 'POST',
      path:
          '/v1/cooperatives/$coop/households/$household/seasons/$season/yield-estimates',
      idempotencyKey: key,
      body: {'expected_seq': expectedSeq, 'rice_t': riceT, 'straw_t': strawT},
    );
    return (result['yield_estimate'] as Map).cast<String, Object?>();
  }

  Future<HouseholdJson> open(
    String coop,
    String household,
    HouseholdJson body,
    String key,
  ) async {
    final error = householdAreaError(body['area_ha'] as String? ?? '');
    if (error != null) throw ArgumentError(error);
    final result = await api.sendJson(
      method: 'POST',
      path: '/v1/cooperatives/$coop/households/$household/seasons',
      idempotencyKey: key,
      body: {...body, 'channel': 'app'},
    );
    return (result['season'] as Map).cast<String, Object?>();
  }
}
