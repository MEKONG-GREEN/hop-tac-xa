import 'package:agrigreen_htx/domain/weather.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/test_env.dart';

// testNow = 01/10/2026 10:00 giờ Việt Nam.
Map<String, Object?> _lunar(int d, int m) => {
  'day': d,
  'month': m,
  'year': 2026,
  'leap': false,
  'label': '${d.toString().padLeft(2, '0')}/${m.toString().padLeft(2, '0')}',
  'first_day': d == 1,
  'full_moon': d == 15,
};

const _source = {
  'provider': 'open_meteo',
  'label': 'Open-Meteo.com',
  'updated_at': '2026-10-01T02:30:00Z',
  'stale': false,
};

Map<String, Object?> _today({String status = 'ok', bool stale = false}) => {
  'cooperative_ref': coopRef,
  'date': '2026-10-01',
  'lunar': _lunar(20, 8),
  'status': status,
  if (status == 'no_location')
    'message': 'Chưa có vị trí để xem thời tiết. Liên hệ Admin để cập nhật.',
  if (status == 'no_data') 'message': 'Chưa có số liệu thời tiết',
  'current': status != 'ok'
      ? null
      : {
          'time': '2026-10-01T03:00:00Z',
          'temperature_c': 31,
          'condition': 'Mưa rào nhẹ',
          'weather_code': 80,
          'precipitation_mm': 0.4,
          'precipitation_probability': 60,
          'wind_speed_kmh': 12,
          'wind_direction': 'TN',
          'humidity_pct': 78,
        },
  'today': status != 'ok'
      ? null
      : {
          'condition': 'Mưa rào',
          'weather_code': 81,
          'temperature_min_c': 25,
          'temperature_max_c': 32,
          'precipitation_mm': 22.0,
          'precipitation_probability_max': 80,
          'wind_speed_max_kmh': 20,
          'wind_direction': 'TN',
          'humidity_pct': 80,
        },
  'source': {..._source, 'stale': stale},
};

Map<String, Object?> _summary(int max) => {
  'condition': 'Nhiều mây',
  'weather_code': 3,
  'temperature_min_c': 25,
  'temperature_max_c': max,
  'precipitation_mm': 1.5,
  'precipitation_probability_max': 40,
  'wind_speed_max_kmh': 15,
  'wind_direction': 'Đ',
  'humidity_pct': 75,
};

Map<String, Object?> _calendar(String month) {
  final days = <Map<String, Object?>>[];
  final y = int.parse(month.substring(0, 4));
  final m = int.parse(month.substring(5, 7));
  final n = DateTime.utc(y, m + 1, 0).day;
  for (var d = 1; d <= n; d++) {
    final date = '$month-${d.toString().padLeft(2, '0')}';
    Map<String, Object?>? weather;
    if (month == '2026-10' && d == 1) {
      weather = {
        'kind': 'forecast',
        'label': '',
        'hourly_available': true,
        ..._summary(32),
        'periods': [
          for (final p in const ['Đêm', 'Sáng', 'Chiều', 'Tối'])
            {
              'name': p,
              'condition': 'Mưa rào',
              'weather_code': 81,
              'temperature_c': 28,
              'precipitation_mm': 5.5,
              'precipitation_probability_max': 80,
              'wind_speed_max_kmh': 20,
            },
        ],
      };
    } else if (month == '2026-10' && d == 2) {
      // EX-01: không có số liệu theo giờ → một khối Cả ngày.
      weather = {
        'kind': 'forecast',
        'label': '',
        'hourly_available': false,
        ..._summary(30),
        'periods': <Object?>[],
      };
    } else if (month == '2026-09' && d == 30) {
      weather = {
        'kind': 'observed',
        'label': 'Thực đo',
        'hourly_available': false,
        ..._summary(33),
        'periods': <Object?>[],
      };
    }
    days.add({
      'date': date,
      'lunar': _lunar((d + 18) % 30 + 1, m - 2),
      'weather': weather,
    });
  }
  return {
    'cooperative_ref': coopRef,
    'month': month,
    'status': 'ok',
    'forecast_days': 7,
    'days': days,
    'source': _source,
  };
}

/// Máy chủ giả trả thời tiết; [online] = false → mất mạng.
class _Weather {
  _Weather(TestEnv env, {this.status = 'ok'}) {
    final base = env.server.onGet!;
    env.server.onGet = (path, q) async {
      if (path == '/v1/weather/today') {
        if (!online) throw NetworkException('offline');
        return _today(status: status);
      }
      if (path == '/v1/weather/calendar') {
        if (!online) throw NetworkException('offline');
        months.add(q?['month'] ?? '');
        return _calendar(q!['month']!);
      }
      return base(path, q);
    };
  }

  String status;
  bool online = true;
  final List<String> months = [];
}

/// Chạm một ô ngày: cuộn danh sách của màn lịch (route trên cùng) về đầu.
Future<void> tapDay(WidgetTester tester, String date) async {
  await tester.drag(find.byType(ListView).last, const Offset(0, 2000));
  await settle(tester);
  final cell = find.byKey(ValueKey('day-$date'));
  await tester.ensureVisible(cell);
  await tester.tap(cell);
  await settle(tester);
}

void main() {
  group('định dạng (BR-04)', () {
    test('mưa một chữ số thập phân, dấu phẩy; ngày dương kèm âm', () {
      expect(rainText(22), '22,0 mm');
      expect(rainText(0.45), '0,5 mm');
      expect(rainText(null), '—');
      expect(windText(12, 'TN'), '12 km/h TN');
      expect(
        dualDate(
          '2026-10-01',
          const LunarDay(day: 20, month: 8, label: '20/08'),
        ),
        '01/10 (20/08 ÂL)',
      );
      expect(
        updatedText(DateTime.utc(2026, 10, 1, 2, 30)),
        'Cập nhật lúc 09:30 01/10',
      );
      expect(longDate('2026-11-23'), 'Thứ Hai, 23/11/2026');
      expect(shiftMonth('2026-01', -1), '2025-12');
      expect(shiftMonth('2026-12', 1), '2027-01');
      expect(monthTitle('2026-10'), 'Tháng 10/2026');
    });
  });

  for (final role in const ['htx_member', 'coop_manager']) {
    testWidgets(
      'AH-225 FN-TT-01: $role thấy Thời tiết hôm nay trên Trang chủ',
      (tester) async {
        final env = TestEnv(roles: [role])..seed();
        final w = _Weather(env);
        await loginToHome(tester, env);
        await settle(tester);
        expect(
          find.text('Thời tiết hôm nay · 01/10 (20/08 ÂL)'),
          findsOneWidget,
        );
        expect(find.text('31°C'), findsOneWidget);
        expect(find.text('Mưa rào nhẹ'), findsOneWidget);
        expect(find.text('25°C – 32°C'), findsOneWidget);
        // AC-04: lượng mưa cả ngày 22.0 → "22,0 mm".
        expect(find.text('22,0 mm'), findsOneWidget);
        expect(find.text('80%'), findsOneWidget);
        expect(find.text('12 km/h TN'), findsOneWidget);
        expect(find.text('Nguồn: Open-Meteo.com'), findsOneWidget);
        expect(find.textContaining('Chưa có nguồn thời tiết'), findsNothing);

        // EX-01: mất mạng → bản đã lưu, hiện "Cập nhật lúc …".
        w.online = false;
        await tester.drag(find.byType(ListView).first, const Offset(0, 300));
        await settle(tester);
        expect(find.text('31°C'), findsOneWidget);
        expect(
          find.text('Nguồn: Open-Meteo.com · Cập nhật lúc 09:30 01/10'),
          findsOneWidget,
        );
        await finish(tester);
      },
    );
  }

  testWidgets(
    'AH-225 EX-02, EX-03: chưa có tọa độ, chưa có số liệu → câu báo kèm ngày',
    (tester) async {
      final env = TestEnv(roles: const ['coop_manager'])..seed();
      final w = _Weather(env, status: 'no_location');
      await loginToHome(tester, env);
      await settle(tester);
      expect(find.text('Thời tiết hôm nay · 01/10 (20/08 ÂL)'), findsOneWidget);
      expect(
        find.text(
          'Chưa có vị trí để xem thời tiết. Liên hệ Admin để cập nhật.',
        ),
        findsOneWidget,
      );
      w.status = 'no_data';
      await tester.drag(find.byType(ListView).first, const Offset(0, 300));
      await settle(tester);
      expect(find.text('Chưa có số liệu thời tiết'), findsOneWidget);
      await finish(tester);
    },
  );

  testWidgets('AH-225: nông dân ngoài BRD — không có thẻ, không gọi API', (
    tester,
  ) async {
    final env = TestEnv()..seed();
    await loginToHome(tester, env);
    await settle(tester);
    expect(find.byKey(const ValueKey('weather-card')), findsNothing);
    expect(find.text('Lịch thời tiết', skipOffstage: false), findsNothing);
    expect(env.server.gets, isNot(contains('/v1/weather/today')));
    await finish(tester);
  });

  testWidgets(
    'AH-226 FN-TT-02: Lịch thời tiết — ngày âm, chi tiết theo buổi, Cả ngày, '
    'tháng trước có Thực đo',
    (tester) async {
      tester.view.physicalSize = const Size(1080, 2400);
      tester.view.devicePixelRatio = 3;
      addTearDown(tester.view.reset);
      final env = TestEnv(roles: const ['htx_member'])..seed();
      final w = _Weather(env);
      await loginToHome(tester, env);
      await settle(tester);
      await openTile(tester, 'Lịch thời tiết');
      await settle(tester);
      expect(find.text('Lịch thời tiết'), findsOneWidget);
      expect(find.text('Tháng 10/2026'), findsOneWidget);
      expect(w.months, ['2026-10']);

      // Mặc định chọn hôm nay: chi tiết theo bốn buổi.
      final detail = find.byKey(const ValueKey('day-detail'));
      await tester.ensureVisible(detail);
      expect(
        find.descendant(of: detail, matching: find.text('Thứ Năm, 01/10/2026')),
        findsOneWidget,
      );
      for (final p in const ['Đêm', 'Sáng', 'Chiều', 'Tối']) {
        expect(
          find.descendant(of: detail, matching: find.text(p)),
          findsOneWidget,
        );
      }
      expect(
        find.descendant(
          of: detail,
          matching: find.textContaining('mưa 5,5 mm'),
        ),
        findsNWidgets(4),
      );

      // EX-01: ngày không có số liệu theo giờ → một khối Cả ngày.
      await tapDay(tester, '2026-10-02');
      await settle(tester);
      await tester.ensureVisible(detail);
      expect(
        find.descendant(of: detail, matching: find.text('Cả ngày')),
        findsOneWidget,
      );
      expect(
        find.descendant(of: detail, matching: find.text('Sáng')),
        findsNothing,
      );

      // Tháng trước: ngày đã qua có nhãn Thực đo.
      await tester.drag(find.byType(ListView).last, const Offset(0, 2000));
      await settle(tester);
      await tester.tap(find.byTooltip('Tháng trước'));
      await settle(tester);
      expect(find.text('Tháng 09/2026'), findsOneWidget);
      expect(w.months, ['2026-10', '2026-09']);
      await tapDay(tester, '2026-09-30');
      await settle(tester);
      await tester.ensureVisible(detail);
      expect(
        find.descendant(of: detail, matching: find.text('Thực đo')),
        findsOneWidget,
      );
      await finish(tester);
    },
  );
}
