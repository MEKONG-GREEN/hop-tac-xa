import 'dart:io';

import 'package:agrigreen_htx/application/household_season_service.dart';
import 'package:agrigreen_htx/ui/widgets.dart'
    show msgTaskSeasonClosed, rejectMessage;
import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/db/local_db.dart';
import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/secure/secure_store.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'support/test_env.dart' show MemoryDbOpener;

// AH-114: tích nhanh khi mất mạng lưu trên máy; vụ đã đóng giữ nhãn, người dùng xóa được.
void main() {
  final season = <String, Object?>{
    'season_ref': 'season',
    'sop_snapshot': {
      'steps': [
        {
          'seq': 1,
          'name': 'Bón lót',
          'materials': [
            {'material_code': 'URE', 'dose_per_ha': '50', 'unit': 'kg'},
          ],
        },
      ],
      // AH-113: việc 1 sửa vật tư, việc 6 tự thêm.
      'custom': {
        'edits': [
          {
            'seq': 1,
            'window': {'kind': 'none'},
            'materials': [
              {'material_code': 'URE', 'dose_per_ha': '60', 'unit': 'kg'},
            ],
          },
        ],
        'added': [
          {
            'seq': 6,
            'name': 'Phun thuốc cỏ',
            'materials': [
              {'name': 'Thuốc cỏ', 'dose_per_ha': '1.5', 'unit': 'l'},
            ],
          },
        ],
      },
    },
  };

  test('việc hiệu lực đọc lớp tùy chỉnh của vụ (AH-113)', () {
    final s = season['sop_snapshot'];
    expect(
      ((HouseholdSeasonService.effectiveStep(s, 1)!['materials'] as List).single
          as Map)['dose_per_ha'],
      '60',
    );
    expect(
      HouseholdSeasonService.effectiveStep(s, 6)!['name'],
      'Phun thuốc cỏ',
    );
    expect(HouseholdSeasonService.effectiveStep(s, 9), isNull);
    expect(HouseholdSeasonService.effectiveStep(null, 1), isNull);
  });

  test(
    'tích nhanh ngoại tuyến → bản ghi "Hoàn tất" hôm nay, vật tư khuyến cáo; '
    'bị từ chối vì vụ đã đóng → nhãn, xóa được; bản chờ gửi không xóa',
    () async {
      final dir = await Directory.systemTemp.createTemp('ah114-tick-');
      final path = '${dir.path}/journal.db';
      final db = await LocalDb.open(
        opener: MemoryDbOpener(file: path),
        path: path,
        keys: DbKeyProvider(MemorySecureStore()),
      );
      final clock = FakeClock(DateTime.utc(2026, 11, 24, 3));
      final outbox = OutboxRepository(
        db: db,
        deviceId: '01910000-0000-7000-8000-000000000001',
        clock: clock,
        ids: UuidV7Generator(),
      );
      final service = HouseholdSeasonService(
        api: HttpSyncApi(
          baseUrl: 'https://api.test',
          accessToken: () async => 'test',
          client: MockClient((_) async => http.Response('{}', 200)),
        ),
        location: const FixedLocationSource(LocationResult(GpsStatus.denied)),
        db: db,
        outbox: outbox,
        journalContext: () async => {
          'recorded_by': 'actor',
          'tenant_id': 'tenant',
          'org_ref': 'coop',
        },
      );
      try {
        expect(service.canQueueOffline, isTrue);
        final tick = await service.queueTick(
          'coop',
          'household',
          season,
          6,
          'event',
          {'source': 'denied'},
          now: DateTime(2026, 11, 24, 10),
        );
        expect(tick.entity, PushEntity.householdTaskRecord);
        expect(tick.payload, containsPair('state', 'done'));
        expect(tick.payload, containsPair('done_on', '2026-11-24'));
        expect(tick.payload, containsPair('step_seq', 6));
        expect(tick.payload, containsPair('expected_season_event_id', 'event'));
        expect(tick.payload, containsPair('season_ref', 'season'));
        expect(tick.payload['materials'], [
          {
            'material_code': '',
            'name': 'Thuốc cỏ',
            'unit': 'l',
            'dose_per_ha': '1.5',
          },
        ]);
        final other = await service.queueTick(
          'coop',
          'household',
          season,
          1,
          'event',
          {'source': 'denied'},
        );
        final rows = await service.pendingTasks('coop', 'household', 'season');
        expect(rows.map((r) => r.status), everyElement(OutboxStatus.pending));
        expect(rows, hasLength(2));
        // Vụ đã đóng trong lúc ngoại tuyến: máy chủ từ chối, bản ghi vẫn trên máy.
        final batch = await outbox.claimBatch(10);
        await outbox.applyResults(batch!.batchKey, [
          PushResult(
            id: tick.id,
            status: 'rejected',
            code: 'cropcycle.task_season_state',
          ),
        ]);
        final rejected = (await service.pendingTasks(
          'coop',
          'household',
          'season',
        )).firstWhere((r) => r.id == tick.id);
        expect(rejected.status, OutboxStatus.rejected);
        expect(rejectMessage(rejected.errorCode), msgTaskSeasonClosed);
        expect(msgTaskSeasonClosed, 'Chưa đồng bộ: vụ đã đóng');
        expect(await service.discardTask(other.id), isFalse);
        expect(await service.discardTask(tick.id), isTrue);
        final left = await service.pendingTasks('coop', 'household', 'season');
        expect(left.map((r) => r.id), [other.id]);
      } finally {
        await db.close();
        await dir.delete(recursive: true);
      }
    },
  );
}
