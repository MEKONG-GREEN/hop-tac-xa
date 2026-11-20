import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:agrigreen_htx/application/household_season_service.dart';
import 'package:agrigreen_htx/ui/screens/household_task_detail_screen.dart';
import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/db/local_db.dart';
import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/reference/reference_repository.dart';
import 'package:agrigreen_mobile/data/secure/secure_store.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'support/test_env.dart' show MemoryDbOpener;

void main() {
  test('AH-99 native journals survive restart, retain device time, and separate accounts', () async {
    final dir = await Directory.systemTemp.createTemp('ah99-task-test-');
    final path = '${dir.path}/journal.db';
    final opener = MemoryDbOpener(file: path);
    final keys = DbKeyProvider(MemorySecureStore());
    var db = await LocalDb.open(opener: opener, path: path, keys: keys);
    final clock = FakeClock(DateTime.utc(2026, 11, 20, 3));
    var actor = 'actor-a';
    var org = 'coop';
    var online = true;
    var forbidden = false;
    final api = HttpSyncApi(
      baseUrl: 'https://api.test',
      accessToken: () async => 'test',
      client: MockClient((r) async {
        if (!online) throw const SocketException('offline');
        if (forbidden) {
          return http.Response(
            '{"code":"forbidden"}',
            403,
            headers: {'content-type': 'application/problem+json'},
          );
        }
        return http.Response(
          jsonEncode(
            r.url.path.endsWith('photo-key')
                ? {
                    'ref': 'household',
                    'public_key': base64Encode(Uint8List(32)),
                  }
                : {
                    'season_ref': 'season',
                    'current_event_id': 'event',
                    'records': [],
                  },
          ),
          200,
          headers: {'content-type': 'application/json'},
        );
      }),
    );
    HouseholdSeasonService service() => HouseholdSeasonService(
      api: api,
      location: const FixedLocationSource(LocationResult(GpsStatus.denied)),
      db: db,
      refs: ReferenceRepository(db, clock),
      outbox: OutboxRepository(
        db: db,
        deviceId: '01910000-0000-7000-8000-000000000001',
        clock: clock,
        ids: UuidV7Generator(),
      ),
      journalContext: () async => {
        'recorded_by': actor,
        'tenant_id': 'tenant',
        'org_ref': org,
      },
    );
    try {
      await service().checklist('coop', 'household', 'season');
      expect(
        await ReferenceRepository(db, clock).publicKeyFor('household'),
        Uint8List(32),
      );
      final first = await service().recordTask('coop', 'household', 'season', {
        'state': 'partial',
        'step_seq': 3,
        'done_on': '2026-11-20',
        'file_refs': <String>[],
        'materials': [
          {'material_code': 'URE', 'dose_per_ha': '1.2345'},
        ],
      });
      expect(first.payload['recorded_by'], 'actor-a');
      expect(first.eventTime, formatRfc3339(clock.now()));
      await db.close();
      db = await LocalDb.open(opener: opener, path: path, keys: keys);
      online = false;
      clock.advance(const Duration(days: 4));
      final rows = await service().pendingTasks('coop', 'household', 'season');
      expect(rows.single.id, first.id);
      expect(rows.single.eventTime, first.eventTime);
      expect(
        (await service().checklist(
          'coop',
          'household',
          'season',
        ))['offline_cache'],
        true,
      );
      await service().preparePhotoKey('household');
      expect(
        await ReferenceRepository(db, clock).publicKeyFor('household'),
        Uint8List(32),
      );
      actor = 'actor-b';
      expect(
        await service().pendingTasks('coop', 'household', 'season'),
        isEmpty,
      );
      await expectLater(
        service().checklist('coop', 'household', 'season'),
        throwsA(isA<NetworkException>()),
      );
      actor = 'actor-a';
      org = 'other-coop';
      expect(
        await service().pendingTasks('coop', 'household', 'season'),
        isEmpty,
      );
      await expectLater(
        service().checklist('coop', 'household', 'season'),
        throwsA(isA<NetworkException>()),
      );
      org = 'coop';
      online = true;
      forbidden = true;
      await expectLater(
        service().checklist('coop', 'household', 'season'),
        throwsA(isA<ApiException>()),
      );
      online = false;
      await expectLater(
        service().checklist('coop', 'household', 'season'),
        throwsA(isA<NetworkException>()),
      );
    } finally {
      await db.close();
      await dir.delete(recursive: true);
    }
  });
  test(
    'AH-99 totals preserve six decimals and reject invalid actual doses',
    () {
      expect(taskActualTotal('1.2345', '0.50'), '0,617250');
      expect(taskActualTotal('-1', '0.50'), isNull);
      expect(taskActualTotal('1.23456', '0.50'), isNull);
      expect(taskActualTotal('0', '0.50'), '0,000000');
    },
  );
}
