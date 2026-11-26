/// AH-152 (FN-06.5): kiểm sớm "Ghi lại vụ" ở app — máy chủ vẫn kiểm lại.
library;

final _pii = RegExp(r'(?:\+?84|0)\d{9,10}|\b\d{12}\b');

/// Bỏ dấu chấm, phẩy, khoảng trắng nhóm nghìn.
String normalizeCost(String raw) => raw.replaceAll(RegExp(r'[\s.,]'), '');

/// Chi phí tùy chọn: số nguyên VND không âm, tối đa 12 chữ số.
String? costError(String raw) {
  final v = normalizeCost(raw);
  if (v.isEmpty) return null;
  return RegExp(r'^[0-9]{1,12}$').hasMatch(v)
      ? null
      : 'Chi phí là số nguyên VND không âm, tối đa 12 chữ số.';
}

/// Ghi chú tùy chọn: tối đa 1000 ký tự, không chứa SĐT hoặc CCCD.
String? noteError(String raw) {
  final v = raw.trim();
  if (v.runes.length > 1000) return 'Ghi chú tối đa 1000 ký tự.';
  if (_pii.hasMatch(v)) return 'Ghi chú không được chứa SĐT hoặc CCCD.';
  return null;
}

/// Số việc sớm, đúng hạn, trễ, bỏ qua, chưa làm (hiển thị riêng từng loại).
String taskLine(Map<String, Object?> t) =>
    'Sớm ${t['early']} · Đúng hạn ${t['on_time']} · Trễ ${t['late']} · '
    'Bỏ qua ${t['skipped']} · Chưa làm ${t['not_done']} (tổng ${t['total']} việc)';

/// Nhóm nghìn kiểu Việt Nam: 15000000 → 15.000.000.
String groupVnd(String digits) {
  final out = StringBuffer();
  for (var i = 0; i < digits.length; i++) {
    if (i > 0 && (digits.length - i) % 3 == 0) out.write('.');
    out.write(digits[i]);
  }
  return out.toString();
}
