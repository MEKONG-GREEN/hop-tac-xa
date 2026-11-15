import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/action_row.dart';
import 'package:agrigreen_mobile/ui/widgets/collapsible_section.dart';
import 'package:agrigreen_mobile/ui/widgets/filter_bar.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

Widget _app(Widget child) => MaterialApp(
  theme: buildTheme(),
  home: Scaffold(body: SingleChildScrollView(child: child)),
);

ActionItem _item(String label, List<String> log, {bool destructive = false}) =>
    ActionItem(
      label: label,
      destructive: destructive,
      onPressed: () => log.add(label),
    );

/// Mọi nút trong [root] không nhỏ hơn 48×48 và hai nút kề nhau trên cùng một
/// hàng cách nhau ≥ 8.
void _expectTouchTargets(WidgetTester tester, Finder buttons) {
  final rects = [
    for (final e in buttons.evaluate()) tester.getRect(find.byWidget(e.widget)),
  ];
  expect(rects, isNotEmpty);
  for (final r in rects) {
    expect(r.width, greaterThanOrEqualTo(48));
    expect(r.height, greaterThanOrEqualTo(48));
  }
  rects.sort((a, b) => a.left.compareTo(b.left));
  for (var i = 1; i < rects.length; i++) {
    final a = rects[i - 1];
    final b = rects[i];
    if ((a.center.dy - b.center.dy).abs() < 1) {
      expect(b.left - a.right, greaterThanOrEqualTo(8));
    }
  }
}

final _buttons = find.byWidgetPredicate(
  (w) =>
      w is FilledButton ||
      w is OutlinedButton ||
      w is TextButton ||
      w is IconButton,
);

void main() {
  group('ActionRow (AC2, AC-N2)', () {
    testWidgets('bốn thao tác: một nút chính, một nút phụ, menu Thêm có hai', (
      tester,
    ) async {
      final log = <String>[];
      await tester.pumpWidget(
        _app(
          ActionRow(
            primary: _item('Sửa', log),
            secondary: _item('Xem', log),
            more: [
              _item('Sao chép', log),
              _item('Xóa', log, destructive: true),
            ],
          ),
        ),
      );
      expect(find.widgetWithText(FilledButton, 'Sửa'), findsOneWidget);
      expect(find.widgetWithText(OutlinedButton, 'Xem'), findsOneWidget);
      expect(find.text('Thêm'), findsOneWidget);
      expect(find.byType(MenuItemButton), findsNothing);
      expect(find.text('Xóa'), findsNothing);

      await tester.tap(find.text('Thêm'));
      await tester.pumpAndSettle();
      expect(find.byType(MenuItemButton), findsNWidgets(2));
      expect(find.widgetWithText(MenuItemButton, 'Sao chép'), findsOneWidget);
      expect(find.widgetWithText(MenuItemButton, 'Xóa'), findsOneWidget);

      await tester.tap(find.widgetWithText(MenuItemButton, 'Xóa'));
      await tester.pumpAndSettle();
      expect(log, ['Xóa']);
    });

    testWidgets(
      'more rỗng thì không có nút Thêm, không có nút phụ khi không khai',
      (tester) async {
        await tester.pumpWidget(
          _app(ActionRow(primary: _item('Xem', <String>[]))),
        );
        expect(find.widgetWithText(FilledButton, 'Xem'), findsOneWidget);
        expect(find.text('Thêm'), findsNothing);
        expect(find.byType(OutlinedButton), findsNothing);
      },
    );

    test('xóa/hủy làm nút chính hoặc nút phụ bị chặn', () {
      final log = <String>[];
      expect(
        () => ActionRow(primary: _item('Xóa', log, destructive: true)),
        throwsAssertionError,
      );
      expect(
        () => ActionRow(
          primary: _item('Sửa', log),
          secondary: _item('Hủy', log, destructive: true),
        ),
        throwsAssertionError,
      );
    });

    testWidgets('nút ≥ 48×48, cách nhau ≥ 8, đạt hướng dẫn vùng chạm', (
      tester,
    ) async {
      final handle = tester.ensureSemantics();
      final log = <String>[];
      await tester.pumpWidget(
        _app(
          ActionRow(
            primary: _item('Sửa', log),
            secondary: _item('Xem', log),
            more: [_item('Xóa', log, destructive: true)],
          ),
        ),
      );
      _expectTouchTargets(tester, _buttons);
      await expectLater(tester, meetsGuideline(androidTapTargetGuideline));
      handle.dispose();
    });
  });

  group('FilterBar (AC2, AC-N3)', () {
    // Màn gọi mẫu: 2 ô trên hàng ('Vụ' đang đặt), 1 ô trong "Lọc thêm".
    Widget harness() {
      final selected = {'Vụ': true, 'Vùng': false, 'Loại': false};
      return StatefulBuilder(
        builder: (context, setState) {
          Widget chip(String label) => ChoiceChip(
            label: Text(label),
            selected: selected[label]!,
            onSelected: (v) => setState(() => selected[label] = v),
          );
          return FilterBar(
            resultCount: 3,
            activeCount: selected.values.where((v) => v).length,
            moreActiveCount: selected['Loại']! ? 1 : 0,
            onClear: () => setState(() => selected.updateAll((_, _) => false)),
            filters: [chip('Vụ'), chip('Vùng')],
            moreFilters: [chip('Loại')],
          );
        },
      );
    }

    testWidgets('hiện số kết quả; Xóa lọc đưa mọi ô về mặc định', (
      tester,
    ) async {
      await tester.pumpWidget(_app(harness()));
      expect(find.text('3 kết quả'), findsOneWidget);
      expect(find.text('Lọc thêm'), findsOneWidget);
      // phần ẩn mặc định đóng
      expect(find.text('Loại'), findsNothing);

      await tester.tap(find.text('Lọc thêm'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Loại'));
      await tester.pumpAndSettle();
      expect(find.text('Lọc thêm (1)'), findsOneWidget);
      expect(
        tester
            .widget<ChoiceChip>(find.widgetWithText(ChoiceChip, 'Vụ'))
            .selected,
        isTrue,
      );

      await tester.tap(find.widgetWithText(TextButton, 'Xóa lọc'));
      await tester.pumpAndSettle();
      for (final label in ['Vụ', 'Vùng', 'Loại']) {
        expect(
          tester
              .widget<ChoiceChip>(find.widgetWithText(ChoiceChip, label))
              .selected,
          isFalse,
          reason: label,
        );
      }
      expect(find.text('Lọc thêm'), findsOneWidget);
      expect(
        tester
            .widget<TextButton>(find.widgetWithText(TextButton, 'Xóa lọc'))
            .onPressed,
        isNull,
      );
    });

    testWidgets('Xóa lọc vô hiệu khi chưa đặt ô nào', (tester) async {
      await tester.pumpWidget(
        _app(
          FilterBar(
            resultCount: 0,
            activeCount: 0,
            onClear: () {},
            filters: const [],
          ),
        ),
      );
      expect(find.text('0 kết quả'), findsOneWidget);
      expect(find.text('Lọc thêm'), findsNothing);
      expect(
        tester
            .widget<TextButton>(find.widgetWithText(TextButton, 'Xóa lọc'))
            .onPressed,
        isNull,
      );
    });

    testWidgets(
      // Font thử (Ahem) rộng hơn font thật nên chỉ dùng một ô ngắn.
      'ô lọc ngắn nằm một hàng trên máy 411 dp, nút ≥ 48×48, cách nhau ≥ 8',
      (tester) async {
        tester.view.physicalSize = const Size(411, 800);
        tester.view.devicePixelRatio = 1;
        addTearDown(tester.view.reset);
        final handle = tester.ensureSemantics();
        await tester.pumpWidget(
          _app(
            FilterBar(
              resultCount: 12,
              activeCount: 1,
              onClear: () {},
              filters: [
                OutlinedButton(onPressed: () {}, child: const Text('Vụ')),
              ],
            ),
          ),
        );
        final centers = {
          for (final f in [
            find.text('Vụ'),
            find.text('12 kết quả'),
            find.text('Xóa lọc'),
          ])
            tester.getCenter(f).dy.round(),
        };
        expect(
          centers,
          hasLength(1),
          reason: 'mọi phần tử của thanh cùng một hàng',
        );
        _expectTouchTargets(tester, _buttons);
        await expectLater(tester, meetsGuideline(androidTapTargetGuideline));
        handle.dispose();
      },
    );
  });

  testWidgets('CollapsibleSection mặc định đóng, chạm tiêu đề thì mở (AC3)', (
    tester,
  ) async {
    await tester.pumpWidget(
      _app(
        const CollapsibleSection(
          title: 'Thông tin thêm',
          child: Text('Nội dung'),
        ),
      ),
    );
    expect(find.text('Thông tin thêm'), findsOneWidget);
    expect(find.text('Nội dung'), findsNothing);
    await tester.tap(find.text('Thông tin thêm'));
    await tester.pumpAndSettle();
    expect(find.text('Nội dung'), findsOneWidget);
  });

  testWidgets('CollapsibleSection giữ giá trị đã nhập khi đóng rồi mở lại', (
    tester,
  ) async {
    await tester.pumpWidget(
      _app(
        const CollapsibleSection(
          title: 'Thông tin thêm',
          child: TextField(key: Key('ghi-chu')),
        ),
      ),
    );
    await tester.tap(find.text('Thông tin thêm'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byKey(const Key('ghi-chu')), 'Ghi chú ruộng');
    await tester.tap(find.text('Thông tin thêm'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Thông tin thêm'));
    await tester.pumpAndSettle();
    expect(find.text('Ghi chú ruộng'), findsOneWidget);
  });

  testWidgets(
    'CollapsibleSection đóng vẫn để Form.validate kiểm trường bắt buộc',
    (tester) async {
      final formKey = GlobalKey<FormState>();
      await tester.pumpWidget(
        _app(
          Form(
            key: formKey,
            child: CollapsibleSection(
              title: 'Thông tin thêm',
              child: TextFormField(
                validator: (v) => (v == null || v.isEmpty) ? 'Bắt buộc' : null,
              ),
            ),
          ),
        ),
      );
      expect(formKey.currentState!.validate(), isFalse);
    },
  );

  testWidgets(
    'CollapsibleSection: mỗi lần validate thất bại thì khối mở và thấy chữ lỗi, kể cả sau khi tự đóng',
    (tester) async {
      final formKey = GlobalKey<FormState>();
      var failedChecks = 0;
      await tester.pumpWidget(
        _app(
          StatefulBuilder(
            builder: (context, setState) => Form(
              key: formKey,
              child: Column(
                children: [
                  CollapsibleSection(
                    title: 'Thông tin thêm',
                    failedChecks: failedChecks,
                    child: TextFormField(
                      validator: (v) => (v == null || v.isEmpty)
                          ? 'Bắt buộc nhập ghi chú'
                          : null,
                    ),
                  ),
                  FilledButton(
                    onPressed: () => setState(() {
                      if (!formKey.currentState!.validate()) failedChecks++;
                    }),
                    child: const Text('Lưu'),
                  ),
                ],
              ),
            ),
          ),
        ),
      );
      expect(find.text('Bắt buộc nhập ghi chú'), findsNothing);
      await tester.tap(find.text('Lưu'));
      await tester.pumpAndSettle();
      expect(failedChecks, 1);
      expect(find.text('Bắt buộc nhập ghi chú'), findsOneWidget);

      // Người dùng tự đóng khối rồi lưu lại khi vẫn còn lỗi: khối phải mở lại.
      await tester.tap(find.text('Thông tin thêm'));
      await tester.pumpAndSettle();
      expect(find.text('Bắt buộc nhập ghi chú').hitTestable(), findsNothing);
      await tester.tap(find.text('Lưu'));
      await tester.pumpAndSettle();
      expect(failedChecks, 2);
      expect(find.text('Bắt buộc nhập ghi chú').hitTestable(), findsOneWidget);
    },
  );
}
