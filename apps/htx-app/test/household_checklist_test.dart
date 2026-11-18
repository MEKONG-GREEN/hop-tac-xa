import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:agrigreen_htx/application/household_season_service.dart';
import 'package:agrigreen_htx/ui/screens/household_checklist_screen.dart';
import 'package:agrigreen_mobile/data/photo/location_source.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

class _SlowGPS implements LocationSource {
  final wait = Completer<LocationResult>();
  @override
  Future<LocationResult> current() => wait.future;
}

Future<List<http.Request>> showChecklist(
  WidgetTester tester, {
  bool write = true,
  bool failOnce = false,
  bool inflight = false,
  LocationSource? gps,
}) async {
  final calls = <http.Request>[];
  final task = <String, Object?>{
    'seq': 1,
    'name': 'Bón phân',
    'completion_status': 'due',
    'can_complete': true,
    'undated': true,
  };
  final before = <String, Object?>{
    'current_event_id': 'event',
    'records': <Object?>[],
    'progress': {
      'as_of_date': '2026-11-18',
      'percent': '0.00',
      'stages': [
        {
          'seq': 1,
          'name': 'Chăm sóc',
          'status': 'not_started',
          'tasks': [task],
        },
      ],
    },
  };
  final record = {
    'id': 'log',
    'step_seq': 1,
    'state': 'done',
    'done_on': '2026-11-18',
    'gps_missing': true,
    'inputs': <Object?>[],
  };
  final after = {
    'current_event_id': 'event',
    'records': [record],
    'progress': {
      'as_of_date': '2026-11-18',
      'percent': '100.00',
      'stages': [
        {
          'seq': 1,
          'name': 'Chăm sóc',
          'status': 'done',
          'tasks': [
            {
              ...task,
              'can_complete': false,
              'completion_status': 'done_on_time',
            },
          ],
        },
      ],
    },
  };
  var isDone = false;
  final service = HouseholdSeasonService(
    api: HttpSyncApi(
      baseUrl: 'https://api.test',
      accessToken: () async => 'token',
      client: MockClient((request) async {
        if (request.method == 'GET') {
          return http.Response.bytes(
            utf8.encode(jsonEncode(isDone ? after : before)),
            200,
            headers: {'content-type': 'application/json'},
          );
        }
        calls.add(request);
        if (failOnce && calls.length == 1) {
          if (inflight) {
            return http.Response.bytes(
              utf8.encode(
                jsonEncode({
                  'type': '',
                  'title': 'Đang xử lý',
                  'status': 409,
                  'code': 'idempotency_in_progress',
                }),
              ),
              409,
              headers: {'content-type': 'application/problem+json'},
            );
          }
          throw const SocketException('offline');
        }
        isDone = true;
        return http.Response.bytes(
          utf8.encode(jsonEncode({'record': record, 'checklist': after})),
          201,
          headers: {'content-type': 'application/json'},
        );
      }),
    ),
    location:
        gps ?? const FixedLocationSource(LocationResult(GpsStatus.denied)),
  );
  await tester.pumpWidget(
    MaterialApp(
      home: HouseholdChecklistScreen(
        service: service,
        coop: 'coop',
        household: 'household',
        canWrite: write,
        season: {
          'season_ref': 'season',
          'season_code': 'DX',
          'season_year': 2030,
          'anchor_date': '2030-01-01',
          'area_ha': '0.50',
          'sop_snapshot': {
            'name': 'SOP',
            'version_no': 1,
            'anchor_name': 'Mốc gốc',
          },
        },
      ),
    ),
  );
  await tester.pumpAndSettle();
  return calls;
}

void main() {
  for (final inflight in [false, true]) {
    testWidgets(
      'AH-88 hủy không ghi, retry giữ key và GPS (inflight=$inflight)',
      (tester) async {
        final calls = await showChecklist(
          tester,
          failOnce: true,
          inflight: inflight,
        );
        await tester.tap(find.byType(CheckboxListTile));
        await tester.pumpAndSettle();
        expect(calls, isEmpty);
        await tester.tap(find.text('Hủy'));
        await tester.pumpAndSettle();
        expect(calls, isEmpty);
        await tester.tap(find.byType(CheckboxListTile));
        await tester.pumpAndSettle();
        await tester.tap(find.text('Xác nhận'));
        await tester.pumpAndSettle();
        expect(calls, hasLength(1));
        await tester.tap(find.text('Xác nhận'));
        await tester.pumpAndSettle();
        expect(calls, hasLength(2));
        expect(
          calls[0].headers['Idempotency-Key'],
          calls[1].headers['Idempotency-Key'],
        );
        expect(calls[0].body, calls[1].body);
        expect((jsonDecode(calls[0].body) as Map)['gps'], {'source': 'denied'});
        expect(find.text('Tiến độ: 100.00%'), findsOneWidget);
        expect(find.textContaining('Thiếu GPS'), findsOneWidget);
        expect(find.byType(CheckboxListTile), findsNothing);
      },
    );
  }
  testWidgets('AH-88 Đại diện chỉ đọc', (tester) async {
    final calls = await showChecklist(tester, write: false);
    expect(find.text('Bón phân'), findsOneWidget);
    expect(find.byType(CheckboxListTile), findsNothing);
    expect(calls, isEmpty);
  });
  testWidgets('AH-88 hai thao tác, GPS chậm không ngăn lưu quá hai giây', (
    tester,
  ) async {
    final gps = _SlowGPS();
    final calls = await showChecklist(tester, gps: gps);
    await tester.tap(find.byType(CheckboxListTile));
    await tester.pump();
    await tester.tap(find.text('Xác nhận'));
    await tester.pump();
    expect(calls, isEmpty);
    await tester.pump(const Duration(seconds: 2));
    await tester.pumpAndSettle();
    expect(calls, hasLength(1));
    expect((jsonDecode(calls[0].body) as Map)['gps'], {'source': 'no_device'});
    gps.wait.complete(const LocationResult(GpsStatus.denied));
    await tester.pumpAndSettle();
    expect(find.text('Tiến độ: 100.00%'), findsOneWidget);
  });
}
