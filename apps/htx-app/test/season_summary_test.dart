import 'package:agrigreen_htx/domain/season_summary.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('AH-152 chi phí tùy chọn, số nguyên VND tối đa 12 chữ số', () {
    expect(costError(''), isNull);
    expect(costError('15.000.000'), isNull);
    expect(normalizeCost('15.000.000'), '15000000');
    expect(costError('-1'), isNotNull);
    expect(costError('1234567890123'), isNotNull);
  });
  test('AH-152 ghi chú không chứa SĐT/CCCD, tối đa 1000 ký tự', () {
    expect(noteError('Vụ được mùa'), isNull);
    expect(noteError('Gọi 0912345678'), isNotNull);
    expect(noteError('CCCD 012345678901'), isNotNull);
    expect(noteError('a' * 1001), isNotNull);
  });
  test('AH-152 nhóm nghìn VND', () {
    expect(groupVnd('15000000'), '15.000.000');
    expect(groupVnd('0'), '0');
    expect(groupVnd('999'), '999');
    expect(groupVnd('1000'), '1.000');
  });
}
