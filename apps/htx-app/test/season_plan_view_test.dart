import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:agrigreen_htx/domain/season_plan.dart';
import 'package:agrigreen_htx/ui/season_plan_view.dart';

void main() {
  testWidgets('AH-71: Không ngày, Mở và chờ mốc hiển thị riêng, chỉ đọc', (
    tester,
  ) async {
    final plan = SeasonPlan.fromJson({
      'as_of_date': '2026-11-18',
      'stages': [
        {
          'seq': 1,
          'name': 'Chăm sóc',
          'steps': [
            {
              'seq': 1,
              'name': 'Theo dõi',
              'undated': true,
              'status': 'due',
              'from_date': null,
              'to_date': null,
            },
            {
              'seq': 2,
              'name': 'Tưới',
              'undated': false,
              'status': 'due',
              'from_date': '2026-11-01',
              'to_date': null,
            },
            {
              'seq': 3,
              'name': 'Sau thu hoạch',
              'undated': false,
              'status': 'waiting_anchor',
              'from_date': null,
              'to_date': null,
            },
          ],
        },
      ],
    });
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: SingleChildScrollView(child: SeasonPlanView(plan: plan)),
        ),
      ),
    );
    expect(find.text('1. Chăm sóc'), findsOneWidget);
    expect(find.text('Chưa đặt ngày'), findsOneWidget);
    expect(find.text('Từ 01/11/2026'), findsOneWidget);
    expect(find.text('Chờ mốc'), findsOneWidget);
    expect(find.text('Đến hạn'), findsNWidgets(2));
    expect(find.text('Trễ hạn'), findsNothing);
    expect(find.byType(ElevatedButton), findsNothing);
    expect(tester.takeException(), isNull);
  });

  test('AH-71: không chấp nhận trạng thái lịch không biết', () {
    expect(
      () => SeasonPlanStep.fromJson({'status': 'guessed'}),
      throwsFormatException,
    );
  });
}
