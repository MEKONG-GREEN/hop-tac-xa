import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

double contrastRatio(Color a, Color b) {
  final la = a.computeLuminance();
  final lb = b.computeLuminance();
  final hi = la > lb ? la : lb;
  final lo = la > lb ? lb : la;
  return (hi + 0.05) / (lo + 0.05);
}

/// Nền nhạt 12% của chính màu chữ (cách Tag của app dựng nền).
Color tint(Color c) => Color.alphaBlend(c.withValues(alpha: .12), Colors.white);

void main() {
  final theme = buildTheme();

  group('AC1/AC-N1: cỡ chữ ≥ 16', () {
    test('mọi kiểu chữ của textTheme', () {
      final t = theme.textTheme;
      final styles = {
        'displayLarge': t.displayLarge,
        'displayMedium': t.displayMedium,
        'displaySmall': t.displaySmall,
        'headlineLarge': t.headlineLarge,
        'headlineMedium': t.headlineMedium,
        'headlineSmall': t.headlineSmall,
        'titleLarge': t.titleLarge,
        'titleMedium': t.titleMedium,
        'titleSmall': t.titleSmall,
        'bodyLarge': t.bodyLarge,
        'bodyMedium': t.bodyMedium,
        'bodySmall': t.bodySmall,
        'labelLarge': t.labelLarge,
        'labelMedium': t.labelMedium,
        'labelSmall': t.labelSmall,
      };
      expect(styles.length, 15);
      styles.forEach((name, s) {
        expect(s?.fontSize, isNotNull, reason: name);
        expect(s!.fontSize, greaterThanOrEqualTo(16), reason: name);
      });
    });

    test(
      'chữ của thanh trên, dòng danh sách, chip, snackbar, tooltip, nút',
      () {
        final sizes = {
          'appBar': theme.appBarTheme.titleTextStyle,
          'listTile.title': theme.listTileTheme.titleTextStyle,
          'listTile.subtitle': theme.listTileTheme.subtitleTextStyle,
          'chip': theme.chipTheme.labelStyle,
          'snackBar': theme.snackBarTheme.contentTextStyle,
          'tooltip': theme.tooltipTheme.textStyle,
          'filled': theme.filledButtonTheme.style?.textStyle?.resolve({}),
          'outlined': theme.outlinedButtonTheme.style?.textStyle?.resolve({}),
          'elevated': theme.elevatedButtonTheme.style?.textStyle?.resolve({}),
          'text': theme.textButtonTheme.style?.textStyle?.resolve({}),
        };
        sizes.forEach((name, s) {
          expect(s?.fontSize, isNotNull, reason: name);
          expect(s!.fontSize, greaterThanOrEqualTo(16), reason: name);
        });
      },
    );
  });

  group('AC1/AC-N1: vùng chạm ≥ 48×48', () {
    test('minimumSize của các nút trong chủ đề', () {
      final minimums = {
        'filled': theme.filledButtonTheme.style?.minimumSize,
        'outlined': theme.outlinedButtonTheme.style?.minimumSize,
        'elevated': theme.elevatedButtonTheme.style?.minimumSize,
        'text': theme.textButtonTheme.style?.minimumSize,
        'icon': theme.iconButtonTheme.style?.minimumSize,
      };
      minimums.forEach((name, m) {
        final size = m?.resolve({});
        expect(size, isNotNull, reason: name);
        expect(size!.width, greaterThanOrEqualTo(48), reason: name);
        expect(size.height, greaterThanOrEqualTo(48), reason: name);
      });
      expect(theme.materialTapTargetSize, MaterialTapTargetSize.padded);
    });

    testWidgets('nút dựng từ chủ đề có kích thước ≥ 48×48', (tester) async {
      final handle = tester.ensureSemantics();
      await tester.pumpWidget(
        MaterialApp(
          theme: theme,
          home: Scaffold(
            body: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                FilledButton(onPressed: () {}, child: const Text('A')),
                OutlinedButton(onPressed: () {}, child: const Text('B')),
                ElevatedButton(onPressed: () {}, child: const Text('C')),
                TextButton(onPressed: () {}, child: const Text('D')),
                IconButton(
                  onPressed: () {},
                  icon: const Icon(Icons.add),
                  tooltip: 'Thêm',
                ),
                Checkbox(value: false, onChanged: (_) {}),
              ],
            ),
          ),
        ),
      );
      for (final type in [
        FilledButton,
        OutlinedButton,
        ElevatedButton,
        TextButton,
        IconButton,
        Checkbox,
      ]) {
        final size = tester.getSize(find.byType(type));
        expect(size.width, greaterThanOrEqualTo(48), reason: '$type');
        expect(size.height, greaterThanOrEqualTo(48), reason: '$type');
      }
      await expectLater(tester, meetsGuideline(androidTapTargetGuideline));
      handle.dispose();
    });
  });

  group('AC1: tương phản chữ/nền ≥ 4,5:1', () {
    const fgs = {
      'ink': AgColors.ink,
      'mute': AgColors.mute,
      'amber': AgColors.amber,
      'red': AgColors.red,
      'blue': AgColors.blue,
      'g700': AgColors.g700,
    };
    for (final e in fgs.entries) {
      test('${e.key} trên trắng, nền chung và nền nhạt 12%', () {
        expect(contrastRatio(e.value, Colors.white), greaterThanOrEqualTo(4.5));
        expect(contrastRatio(e.value, AgColors.bg), greaterThanOrEqualTo(4.5));
        expect(
          contrastRatio(e.value, tint(e.value)),
          greaterThanOrEqualTo(4.5),
        );
      });
    }

    test('mọi cặp màu của nhãn trạng thái', () {
      for (final status in OutboxStatus.values) {
        final (bg, fg) = statusChipColors(status);
        expect(
          contrastRatio(fg, bg),
          greaterThanOrEqualTo(4.5),
          reason: '$status',
        );
      }
    });

    test('chữ trắng trên g700, red, g900', () {
      for (final bg in [AgColors.g700, AgColors.red, AgColors.g900]) {
        expect(contrastRatio(Colors.white, bg), greaterThanOrEqualTo(4.5));
      }
    });
  });

  testWidgets('StatusChip mọi trạng thái có chữ ≥ 16', (tester) async {
    for (final status in OutboxStatus.values) {
      await tester.pumpWidget(
        MaterialApp(home: Scaffold(body: StatusChip(status))),
      );
      final style = tester.widget<Text>(find.text(status.label)).style!;
      expect(style.fontSize, greaterThanOrEqualTo(16), reason: '$status');
    }
  });
}
