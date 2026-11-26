import 'dart:io';

import 'package:agrigreen_htx/application/household_season_service.dart';
import 'package:agrigreen_htx/ui/screens/household_harvest_card.dart';
import 'package:agrigreen_mobile/core/clock.dart';
import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/db/local_db.dart';
import 'package:agrigreen_mobile/data/outbox/outbox_repository.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/secure/secure_store.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'support/test_env.dart' show MemoryDbOpener;

// AH-190: khai báo sản lượng khi mất mạng — lưu trên máy "Chờ đồng bộ", không khai
// trùng; bị máy chủ từ chối giữ bản khai kèm lý do, xóa được hoặc khai lại.
void main() {
  late Directory dir;
  late LocalDb db;
  late OutboxRepository outbox;
  late HouseholdSeasonService service;

  Future<void> setUpService() async {
    dir = await Directory.systemTemp.createTemp('ah190-');
    final path = '${dir.path}/harvest.db';
    db = await LocalDb.open(
      opener: MemoryDbOpener(file: path),
      path: path,
      keys: DbKeyProvider(MemorySecureStore()),
    );
    outbox = OutboxRepository(
      db: db,
      deviceId: '01910000-0000-7000-8000-000000000001',
      clock: FakeClock(DateTime.utc(2026, 9, 25, 3)),
      ids: UuidV7Generator(),
    );
    service = HouseholdSeasonService(
      api: HttpSyncApi(
        baseUrl: 'https://api.test',
        accessToken: () async => 'test',
        // Mất mạng: mọi lời gọi lỗi kết nối.
        client: MockClient((_) async => throw http.ClientException('offline')),
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
  }

  Future<void> tearDownService() async {
    await db.close();
    await dir.delete(recursive: true);
  }

  test('xếp hàng đúng hợp đồng household_harvest; không khai trùng; bị từ chối giữ '
      'lý do, xóa được; bản chờ gửi không xóa', () async {
    await setUpService();
    try {
      final r = await service.queueHarvest(
        'coop',
        'household',
        'season',
        massT: '3,5',
        harvestDate: '2026-09-20',
        zeroConfirmed: false,
        gps: {'source': 'denied'},
      );
      expect(r.entity, PushEntity.householdHarvest);
      expect(r.payload, {
        'recorded_by': 'actor',
        'tenant_id': 'tenant',
        'cooperative_ref': 'coop',
        'household_ref': 'household',
        'season_ref': 'season',
        'mass_t': '3.5',
        'harvest_date': '2026-09-20',
        'zero_confirmed': false,
        'gps': {'source': 'denied'},
      });
      expect(r.status, OutboxStatus.pending);
      await expectLater(
        service.queueHarvest(
          'coop',
          'household',
          'season',
          massT: '4',
          harvestDate: '2026-09-20',
          zeroConfirmed: false,
          gps: {'source': 'denied'},
        ),
        throwsA(isA<StateError>()),
      );
      expect(
        () => service.queueHarvest(
          'coop',
          'household',
          'other',
          massT: '-1',
          harvestDate: '2026-09-20',
          zeroConfirmed: false,
          gps: {'source': 'denied'},
        ),
        throwsA(isA<ArgumentError>()),
      );
      expect((await service.pendingHarvests('coop', 'household')).keys, [
        'season',
      ]);
      expect(await service.discardHarvest(r.id), isFalse);
      // Vụ đổi trạng thái trong lúc mất mạng: máy chủ từ chối, bản khai còn trên máy.
      final batch = await outbox.claimBatch(10);
      await outbox.applyResults(batch!.batchKey, [
        PushResult(
          id: r.id,
          status: 'rejected',
          code: 'cropcycle.season_state',
        ),
      ]);
      final rejected = await service.pendingHarvest(
        'coop',
        'household',
        'season',
      );
      expect(rejected!.status, OutboxStatus.rejected);
      expect(
        harvestRejectMessage(rejected.errorCode),
        contains('không còn "Đang canh tác"'),
      );
      expect(await service.discardHarvest(r.id), isTrue);
      expect(
        await service.pendingHarvest('coop', 'household', 'season'),
        isNull,
      );
    } finally {
      await tearDownService();
    }
  });

  testWidgets('mất mạng khi lưu → lưu trên máy, báo Chờ đồng bộ', (
    tester,
  ) async {
    await tester.runAsync(setUpService);
    OutboxRecord? queued;
    final season = <String, Object?>{
      'season_ref': 'season',
      'household_ref': 'household',
      'anchor_date': '2026-06-01',
      'current_event_id': 'event',
      'status': 'open',
    };
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: StatefulBuilder(
            builder: (context, setState) => queued == null
                ? HouseholdHarvestCard(
                    service: service,
                    coop: 'coop',
                    season: season,
                    onDeclared: (_) {},
                    onQueued: (r) => setState(() => queued = r),
                  )
                : HouseholdHarvestPending(
                    record: queued!,
                    canRedo: true,
                    onDiscard: () {},
                    onRedo: () {},
                  ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('Khai báo sản lượng'));
    await tester.pump();
    await tester.enterText(
      find.widgetWithText(TextField, 'Tổng sản lượng (tấn)'),
      '3,5',
    );
    await tester.enterText(
      find.widgetWithText(TextField, 'Ngày thu hoạch cuối (dd/mm/yyyy)'),
      '20/09/2026',
    );
    await tester.tap(find.text('Lưu khai báo'));
    for (var i = 0; i < 60 && queued == null; i++) {
      await tester.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 20)),
      );
      await tester.pump(const Duration(seconds: 1));
    }
    await tester.pump();
    if (queued == null) {
      debugPrint(
        tester
            .widgetList<Text>(find.byType(Text))
            .map((t) => t.data)
            .join(' | '),
      );
    }
    expect(queued?.payload['mass_t'], '3.5');
    expect(queued?.payload['harvest_date'], '2026-09-20');
    expect(find.text('Đã lưu trên máy — Chờ đồng bộ'), findsOneWidget);
    expect(find.text('3,5 tấn · thu hoạch 20/09/2026'), findsOneWidget);
    await tester.runAsync(tearDownService);
  });

  testWidgets('bản khai bị từ chối: lý do, xóa, khai lại', (tester) async {
    var discarded = false, redo = false;
    final record = OutboxRecord(
      id: 'r1',
      seq: 1,
      idemKey: 'k',
      entity: PushEntity.householdHarvest,
      op: RecordOp.create,
      eventTime: '2026-09-25T10:00:00+07:00',
      recordedAt: DateTime.utc(2026, 9, 25),
      payload: {'mass_t': '3.5', 'harvest_date': '2026-09-20'},
      status: OutboxStatus.rejected,
      errorCode: 'cropcycle.season_state',
    );
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: HouseholdHarvestPending(
            record: record,
            canRedo: true,
            onDiscard: () => discarded = true,
            onRedo: () => redo = true,
          ),
        ),
      ),
    );
    expect(find.text('Bản khai trên máy — Không đồng bộ được'), findsOneWidget);
    expect(find.textContaining('không còn "Đang canh tác"'), findsOneWidget);
    await tester.tap(find.text('Xóa bản khai trên máy'));
    await tester.tap(find.text('Khai lại'));
    expect(discarded && redo, isTrue);
  });
}
