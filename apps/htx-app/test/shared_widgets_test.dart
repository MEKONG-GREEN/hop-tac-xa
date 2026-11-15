import 'package:agrigreen_htx/ui/widgets.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

/// AH-24 AC1: chữ của thành phần dùng chung trong app không nhỏ hơn 16.
void main() {
  testWidgets('Tag có chữ ≥ 16', (tester) async {
    await tester.pumpWidget(
      const MaterialApp(home: Scaffold(body: Tag('Chờ gửi'))),
    );
    final style = tester.widget<Text>(find.text('Chờ gửi')).style!;
    expect(style.fontSize, greaterThanOrEqualTo(16));
  });
}
