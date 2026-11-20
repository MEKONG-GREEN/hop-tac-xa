import 'dart:typed_data';

import 'package:agrigreen_mobile/data/photo/photo_pipeline.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:image/image.dart' as img;

void main() {
  test('AH-99 compression retains watermark in an image bounded by 1 MiB', () {
    final source = img.Image(width: 1920, height: 1080);
    img.fill(source, color: img.ColorRgb8(20, 160, 40));
    final encoded = img.encodeJpg(source, quality: 100);
    final out = compressWatermarkedImage(
      encoded,
      'AgriGreen 2026-11-20T10:00:00+07:00\nGPS: 10.123456, 105.123456',
      1024 * 1024,
    );
    expect(out, isNotNull);
    expect(out!.length, lessThanOrEqualTo(1024 * 1024));
    final decoded = img.decodeImage(out)!;
    expect(decoded.width, 1920);
    // The original is solid green; the stamped lower band must contain white
    // text and dark background after JPEG decoding.
    var white = 0, dark = 0;
    for (var y = decoded.height - 160; y < decoded.height; y++) {
      for (var x = 0; x < decoded.width; x++) {
        final p = decoded.getPixel(x, y);
        if (p.r > 200 && p.g > 200 && p.b > 200) white++;
        if (p.r < 30 && p.g < 60 && p.b < 30) dark++;
      }
    }
    expect(white, greaterThan(100));
    expect(dark, greaterThan(100));
  });
  test(
    'AH-99 corrupt images and impossible compression have no unmarked fallback',
    () {
      expect(
        compressWatermarkedImage(
          Uint8List.fromList([1, 2, 3]),
          'stamp',
          1024 * 1024,
        ),
        isNull,
      );
      final image = img.encodeJpg(img.Image(width: 640, height: 480));
      expect(compressWatermarkedImage(image, 'stamp', 1), isNull);
    },
  );
}
