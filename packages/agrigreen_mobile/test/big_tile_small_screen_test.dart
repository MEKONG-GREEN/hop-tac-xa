import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

/// Máy Android giá rẻ: ô lưới trên màn hình chủ chỉ còn ~114×82 px
/// (đo được trên emulator API 29 của CI). Ô lối tắt không được tràn.
void main() {
  for (final size in const [Size(114, 82), Size(96, 72), Size(160, 120)]) {
    testWidgets('BigTile không tràn trong ô ${size.width}×${size.height}', (
      tester,
    ) async {
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Center(
              child: SizedBox.fromSize(
                size: size,
                child: BigTile(
                  icon: Icons.assignment,
                  label: 'Nhiệm vụ hỗ trợ',
                  badge: '12',
                  onTap: () {},
                ),
              ),
            ),
          ),
        ),
      );
      expect(tester.takeException(), isNull);
      expect(find.text('Nhiệm vụ hỗ trợ'), findsOneWidget);
      // AH-24 AC1: chỉ biểu tượng co lại, chữ nhãn giữ cỡ ≥ 16 (không nằm trong FittedBox).
      final label = tester.widget<Text>(find.text('Nhiệm vụ hỗ trợ'));
      expect(label.style!.fontSize, greaterThanOrEqualTo(16));
      expect(
        find.ancestor(
          of: find.text('Nhiệm vụ hỗ trợ'),
          matching: find.byType(FittedBox),
        ),
        findsNothing,
      );
    });
  }
}
