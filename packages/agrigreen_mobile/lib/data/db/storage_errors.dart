/// Nhận biết lỗi hết dung lượng thiết bị khi lưu cục bộ (AH-114 AC6): SQLite
/// SQLITE_FULL (mã 13) hoặc hệ thống tệp ENOSPC (errno 28). Dùng chuỗi lỗi vì
/// sqflite/sqlcipher và dart:io báo lỗi bằng các lớp khác nhau trên mỗi nền tảng.
bool isStorageFull(Object error) {
  final text = '$error';
  return text.contains('SQLITE_FULL') ||
      text.contains('database or disk is full') ||
      text.contains('(code 13)') ||
      text.contains('No space left on device') ||
      text.contains('ENOSPC') ||
      RegExp(r'errno\s*=\s*28\b').hasMatch(text);
}

/// Câu cảnh báo khi thiết bị hết dung lượng: bản ghi mới chưa lưu được, các bản
/// ghi đã chờ đồng bộ vẫn còn nguyên.
const msgStorageFull =
    'Thiết bị đã hết dung lượng nên chưa lưu được. Hãy xóa bớt ảnh hoặc ứng dụng '
    'không dùng rồi lưu lại. Các bản ghi đang chờ đồng bộ vẫn được giữ.';
