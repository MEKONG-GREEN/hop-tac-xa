import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';

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

/// Online-only household operations. Deliberately has no outbox dependency.
class HouseholdSeasonService {
  const HouseholdSeasonService({required this.api, required this.location});
  final SyncApi api;
  final LocationSource location;

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
  ) => api.getJson(
    '/v1/cooperatives/$coop/households/$household/seasons/$season/checklist',
  );

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
      _pages('/v1/masterdata/catalogs/$catalog');
  Future<List<HouseholdJson>> seasons(String coop, String household) =>
      _pages('/v1/cooperatives/$coop/households/$household/seasons');

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
