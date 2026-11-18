import 'dart:convert';
import 'dart:io';

import 'package:agrigreen_htx/application/household_season_service.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

HouseholdSeasonService service(
  Future<http.Response> Function(http.Request) handle,
) => HouseholdSeasonService(
  api: HttpSyncApi(
    baseUrl: 'https://api.test',
    accessToken: () async => 'token',
    client: MockClient(handle),
  ),
  location: const FixedLocationSource(LocationResult(GpsStatus.denied)),
);

void main() {
  test('AH-61 rejects non-positive, precision-losing and non-decimal area', () {
    for (final raw in [
      '0',
      '-1',
      '0.001',
      '0.25001',
      '1e-2',
      '1,25',
      'NaN',
      ' 0.25',
    ]) {
      expect(householdAreaError(raw), isNotNull);
    }
    for (final raw in ['0.01', '1', '1.50']) {
      expect(householdAreaError(raw), isNull);
    }
  });
  test('AH-61 preserves conflict area and retries with the same key/body, GPS denial does not block', () async {
    final requests = <http.Request>[];
    final s = service((request) async {
      requests.add(request);
      return http.Response.bytes(
        utf8.encode(
          jsonEncode({
            'code': 'cropcycle.household_area_exceeded',
            'title': 'Diện tích vượt',
            'remaining_area_ha': '0.20',
          }),
        ),
        409,
        headers: {'content-type': 'application/problem+json'},
      );
    });
    final gps = await s.gps();
    expect(gps, {'source': 'denied'});
    final body = <String, Object?>{
      'area_ha': '0.25',
      'anchor_date': '2030-01-01',
      'gps': gps,
    };
    for (var i = 0; i < 2; i++) {
      try {
        await s.open('coop', 'household', body, 'same-operation-key');
        fail('must return conflict');
      } on ApiException catch (e) {
        expect(e.problem['remaining_area_ha'], '0.20');
      }
    }
    expect(requests.length, 2);
    expect(
      requests[0].headers['Idempotency-Key'],
      requests[1].headers['Idempotency-Key'],
    );
    expect(requests[0].body, requests[1].body);
    expect(
      (jsonDecode(requests[0].body) as Map<String, Object?>)['channel'],
      'app',
    );
  });
  test(
    'AH-61 opening requires network and never falls back to the sync queue',
    () async {
      var attempts = 0;
      final s = service((request) async {
        attempts++;
        throw const SocketException('offline');
      });
      await expectLater(
        s.open('coop', 'household', {'area_ha': '0.25'}, 'online-only-key'),
        throwsA(isA<NetworkException>()),
      );
      expect(attempts, 1);
    },
  );
  test(
    'AH-61 specific active SOP wins; wrong variety and other HTX are excluded',
    () async {
      final s = service((request) async {
        final path = request.url.path;
        final body = switch (path) {
          '/v1/cropcycle/sops' => {
            'items': [
              {
                'ref': 'generic',
                'crop': 'LUA',
                'scope': 'system',
                'published_version': 1,
              },
              {
                'ref': 'specific',
                'name': 'Riêng OM18',
                'crop': 'LUA',
                'variety_code': 'OM18',
                'scope': 'system',
                'published_version': 2,
                'anchor_name': 'Ngày xuống giống',
              },
              {
                'ref': 'wrong-variety',
                'crop': 'LUA',
                'variety_code': 'ST25',
                'scope': 'system',
                'published_version': 1,
              },
              {
                'ref': 'private',
                'crop': 'LUA',
                'scope': 'htx',
                'cooperative_ref': 'other',
                'published_version': 1,
              },
            ],
          },
          '/v1/cropcycle/variety-bindings' => {'items': []},
          '/v1/cropcycle/sops/specific' => {
            'versions': [
              {'id': 'active', 'version_no': 2, 'state': 'active'},
              {'id': 'old', 'version_no': 1, 'state': 'retired'},
            ],
          },
          _ => throw StateError('unexpected request $path'),
        };
        return http.Response.bytes(
          utf8.encode(jsonEncode(body)),
          200,
          headers: {'content-type': 'application/json'},
        );
      });
      final choices = await s.sops('coop', 'LUA', 'OM18');
      expect(choices.map((s) => s.id).toList(), ['active']);
    },
  );
}
