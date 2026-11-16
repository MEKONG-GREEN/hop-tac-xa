import 'package:flutter/material.dart';

/// Màu theo prototype App_Khuyen_Nong_v5.0 (mobile).
abstract final class AgColors {
  static const g900 = Color(0xFF0B4A2F);
  static const g700 = Color(0xFF146B41);
  static const g600 = Color(0xFF1B8A54);
  static const g100 = Color(0xFFE7F5EC);
  // Màu dùng làm màu chữ: tối hơn prototype để đạt tương phản ≥ 4,5:1 (AH-24).
  static const amber = Color(0xFF92400E);
  static const amberBg = Color(0xFFFEF6E7);
  static const red = Color(0xFFB42318);
  static const redBg = Color(0xFFFDECEC);
  static const blue = Color(0xFF0369A1);
  static const ink = Color(0xFF111827);
  static const mute = Color(0xFF4B5563);
  static const bg = Color(0xFFF5F8F6);
}

/// Chủ đề chữ lớn cho máy Android cấu hình thấp, dùng ngoài nắng: chữ ≥ 16,
/// vùng chạm ≥ 48×48 dp, tương phản chữ/nền ≥ 4,5:1 (docs/conventions/giao-dien-chung.md).
ThemeData buildTheme() {
  final scheme = ColorScheme.fromSeed(
    seedColor: AgColors.g700,
    primary: AgColors.g700,
    error: AgColors.red,
    surface: Colors.white,
  );
  const big = Size(64, 56);
  const touch = Size(48, 48);
  const buttonText = TextStyle(fontSize: 18, fontWeight: FontWeight.w600);
  return ThemeData(
    colorScheme: scheme,
    scaffoldBackgroundColor: AgColors.bg,
    visualDensity: VisualDensity.standard,
    materialTapTargetSize: MaterialTapTargetSize.padded,
    textTheme: const TextTheme(
      displayLarge: TextStyle(fontSize: 57, color: AgColors.ink),
      displayMedium: TextStyle(fontSize: 45, color: AgColors.ink),
      displaySmall: TextStyle(fontSize: 36, color: AgColors.ink),
      headlineLarge: TextStyle(fontSize: 32, color: AgColors.ink),
      headlineMedium: TextStyle(fontSize: 28, color: AgColors.ink),
      headlineSmall: TextStyle(fontSize: 24, color: AgColors.ink),
      bodyLarge: TextStyle(fontSize: 18, color: AgColors.ink),
      bodyMedium: TextStyle(fontSize: 16, color: AgColors.ink),
      bodySmall: TextStyle(fontSize: 16, color: AgColors.ink),
      titleLarge: TextStyle(fontSize: 22, fontWeight: FontWeight.w700),
      titleMedium: TextStyle(fontSize: 18, fontWeight: FontWeight.w600),
      titleSmall: TextStyle(fontSize: 16, fontWeight: FontWeight.w600),
      labelLarge: TextStyle(fontSize: 18, fontWeight: FontWeight.w600),
      labelMedium: TextStyle(fontSize: 16, color: AgColors.ink),
      labelSmall: TextStyle(fontSize: 16, color: AgColors.ink),
    ),
    appBarTheme: const AppBarTheme(
      backgroundColor: AgColors.g700,
      foregroundColor: Colors.white,
      titleTextStyle: TextStyle(
        fontSize: 20,
        fontWeight: FontWeight.w700,
        color: Colors.white,
      ),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        minimumSize: big,
        textStyle: buttonText,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(
        minimumSize: big,
        textStyle: buttonText,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      ),
    ),
    elevatedButtonTheme: ElevatedButtonThemeData(
      style: ElevatedButton.styleFrom(
        minimumSize: big,
        textStyle: buttonText,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      ),
    ),
    textButtonTheme: TextButtonThemeData(
      style: TextButton.styleFrom(minimumSize: touch, textStyle: buttonText),
    ),
    iconButtonTheme: IconButtonThemeData(
      style: IconButton.styleFrom(minimumSize: touch),
    ),
    chipTheme: const ChipThemeData(
      labelStyle: TextStyle(fontSize: 16, color: AgColors.ink),
    ),
    snackBarTheme: const SnackBarThemeData(
      contentTextStyle: TextStyle(fontSize: 16, color: Colors.white),
    ),
    tooltipTheme: const TooltipThemeData(
      textStyle: TextStyle(fontSize: 16, color: Colors.white),
    ),
    inputDecorationTheme: const InputDecorationTheme(
      border: OutlineInputBorder(),
      filled: true,
      fillColor: Colors.white,
      contentPadding: EdgeInsets.symmetric(horizontal: 16, vertical: 18),
    ),
    listTileTheme: const ListTileThemeData(
      minVerticalPadding: 14,
      titleTextStyle: TextStyle(
        fontSize: 18,
        fontWeight: FontWeight.w600,
        color: AgColors.ink,
      ),
      subtitleTextStyle: TextStyle(fontSize: 16, color: AgColors.mute),
    ),
  );
}
