/// Thời tiết của HTX (BRD Thời tiết & Thu gom phụ phẩm, FN-TT-01, FN-TT-02).
///
/// Máy chủ đã làm tròn số theo BR-04 (°C và km/h số nguyên, mm một chữ số
/// thập phân, % số nguyên) và tính sẵn ngày âm lịch theo giờ Việt Nam — app
/// chỉ hiển thị, không tính lại.
library;

/// Ngày âm lịch máy chủ trả kèm mỗi ngày dương.
class LunarDay {
  const LunarDay({
    required this.day,
    required this.month,
    required this.label,
    this.leap = false,
    this.firstDay = false,
    this.fullMoon = false,
  });

  factory LunarDay.fromJson(Object? raw) {
    final j = _map(raw);
    return LunarDay(
      day: _int(j['day']) ?? 0,
      month: _int(j['month']) ?? 0,
      label: '${j['label'] ?? ''}',
      leap: j['leap'] == true,
      firstDay: j['first_day'] == true,
      fullMoon: j['full_moon'] == true,
    );
  }

  final int day;
  final int month;

  /// `dd/mm`, thêm ` nhuận` với tháng nhuận.
  final String label;
  final bool leap;
  final bool firstDay;
  final bool fullMoon;
}

/// Số liệu một ngày, một buổi hoặc hiện tại; trường null → hiện "—".
class WeatherFigures {
  const WeatherFigures({
    this.condition = '',
    this.temperatureC,
    this.temperatureMinC,
    this.temperatureMaxC,
    this.precipitationMm,
    this.precipitationProbability,
    this.windSpeedKmh,
    this.windDirection = '',
    this.humidityPct,
  });

  factory WeatherFigures.fromJson(Object? raw) {
    final j = _map(raw);
    return WeatherFigures(
      condition: '${j['condition'] ?? ''}',
      temperatureC: _int(j['temperature_c']),
      temperatureMinC: _int(j['temperature_min_c']),
      temperatureMaxC: _int(j['temperature_max_c']),
      precipitationMm: _num(j['precipitation_mm']),
      precipitationProbability:
          _int(j['precipitation_probability']) ??
          _int(j['precipitation_probability_max']),
      windSpeedKmh: _int(j['wind_speed_kmh']) ?? _int(j['wind_speed_max_kmh']),
      windDirection: '${j['wind_direction'] ?? ''}',
      humidityPct: _int(j['humidity_pct']),
    );
  }

  final String condition;
  final int? temperatureC;
  final int? temperatureMinC;
  final int? temperatureMaxC;
  final num? precipitationMm;
  final int? precipitationProbability;
  final int? windSpeedKmh;
  final String windDirection;
  final int? humidityPct;
}

/// Nguồn và độ mới của số liệu (FN-TT-01/EX-01).
class WeatherSource {
  const WeatherSource({
    required this.label,
    this.updatedAt,
    this.stale = false,
  });

  factory WeatherSource.fromJson(Object? raw) {
    final j = _map(raw);
    final at = j['updated_at'];
    return WeatherSource(
      label: '${j['label'] ?? ''}',
      updatedAt: at is String ? DateTime.tryParse(at) : null,
      stale: j['stale'] == true,
    );
  }

  final String label;
  final DateTime? updatedAt;

  /// Lần lấy gần nhất lỗi hoặc số liệu cũ: hiện "Cập nhật lúc …".
  final bool stale;
}

/// Trạng thái số liệu của HTX.
abstract final class WeatherStatus {
  static const ok = 'ok';
  static const noLocation = 'no_location';
  static const noData = 'no_data';
}

/// Câu báo nguyên văn BRD khi máy chủ không trả `message`.
const msgNoLocation =
    'Chưa có vị trí để xem thời tiết. Liên hệ Admin để cập nhật.';
const msgNoData = 'Chưa có số liệu thời tiết';

String _message(Map<String, Object?> j, String status) {
  final m = j['message'];
  if (m is String && m.isNotEmpty) return m;
  return switch (status) {
    WeatherStatus.noLocation => msgNoLocation,
    WeatherStatus.noData => msgNoData,
    _ => '',
  };
}

/// Thẻ Thời tiết hôm nay (FN-TT-01).
class WeatherToday {
  const WeatherToday({
    required this.date,
    required this.lunar,
    required this.status,
    required this.message,
    required this.source,
    this.current,
    this.today,
    this.fresh = true,
  });

  factory WeatherToday.fromJson(Map<String, Object?> j, {bool fresh = true}) {
    final status = '${j['status'] ?? WeatherStatus.noData}';
    return WeatherToday(
      date: '${j['date'] ?? ''}',
      lunar: LunarDay.fromJson(j['lunar']),
      status: status,
      message: _message(j, status),
      source: WeatherSource.fromJson(j['source']),
      current: j['current'] == null
          ? null
          : WeatherFigures.fromJson(j['current']),
      today: j['today'] == null ? null : WeatherFigures.fromJson(j['today']),
      fresh: fresh,
    );
  }

  /// `YYYY-MM-DD` theo giờ Việt Nam.
  final String date;
  final LunarDay lunar;
  final String status;
  final String message;
  final WeatherSource source;
  final WeatherFigures? current;
  final WeatherFigures? today;

  /// false: đang xem bản đã lưu khi ngoại tuyến (EX-01).
  final bool fresh;

  bool get ok => status == WeatherStatus.ok;

  /// Hiện "Cập nhật lúc …" khi số liệu không mới (EX-01).
  bool get showUpdatedAt => !fresh || source.stale;
}

/// Một buổi trong ngày (Đêm, Sáng, Chiều, Tối).
class WeatherPeriod {
  const WeatherPeriod(this.name, this.figures);

  factory WeatherPeriod.fromJson(Object? raw) {
    final j = _map(raw);
    return WeatherPeriod('${j['name'] ?? ''}', WeatherFigures.fromJson(j));
  }

  final String name;
  final WeatherFigures figures;
}

/// Số liệu của một ngày trên lịch: dự báo, thực đo hoặc dự báo cuối ngày.
class WeatherDay {
  const WeatherDay({
    required this.kind,
    required this.label,
    required this.summary,
    required this.periods,
    required this.hourly,
  });

  factory WeatherDay.fromJson(Object? raw) {
    final j = _map(raw);
    final periods = j['periods'];
    return WeatherDay(
      kind: '${j['kind'] ?? ''}',
      label: '${j['label'] ?? ''}',
      summary: WeatherFigures.fromJson(j),
      periods: [
        if (periods is List)
          for (final p in periods) WeatherPeriod.fromJson(p),
      ],
      hourly: j['hourly_available'] == true,
    );
  }

  final String kind;

  /// "Thực đo", "Dự báo cuối ngày" hoặc rỗng với ngày dự báo (BR-04).
  final String label;
  final WeatherFigures summary;
  final List<WeatherPeriod> periods;

  /// false: chỉ có số liệu cả ngày, chi tiết hiện một khối (EX-01).
  final bool hourly;
}

class CalendarDay {
  const CalendarDay({required this.date, required this.lunar, this.weather});

  factory CalendarDay.fromJson(Object? raw) {
    final j = _map(raw);
    return CalendarDay(
      date: '${j['date'] ?? ''}',
      lunar: LunarDay.fromJson(j['lunar']),
      weather: j['weather'] == null ? null : WeatherDay.fromJson(j['weather']),
    );
  }

  /// `YYYY-MM-DD`.
  final String date;
  final LunarDay lunar;
  final WeatherDay? weather;

  int get day =>
      int.tryParse(date.length >= 10 ? date.substring(8, 10) : '') ?? 0;
}

/// Lịch thời tiết một tháng (FN-TT-02).
class WeatherCalendar {
  const WeatherCalendar({
    required this.month,
    required this.status,
    required this.message,
    required this.days,
    required this.source,
    this.fresh = true,
  });

  factory WeatherCalendar.fromJson(
    Map<String, Object?> j, {
    bool fresh = true,
  }) {
    final status = '${j['status'] ?? WeatherStatus.noData}';
    final days = j['days'];
    return WeatherCalendar(
      month: '${j['month'] ?? ''}',
      status: status,
      message: _message(j, status),
      days: [
        if (days is List)
          for (final d in days) CalendarDay.fromJson(d),
      ],
      source: WeatherSource.fromJson(j['source']),
      fresh: fresh,
    );
  }

  /// `YYYY-MM`.
  final String month;
  final String status;
  final String message;
  final List<CalendarDay> days;
  final WeatherSource source;
  final bool fresh;
}

// ---- Định dạng hiển thị (vi-VN) ----

const _dash = '—';

String tempText(int? c) => c == null ? _dash : '$c°C';

String rangeText(int? min, int? max) =>
    min == null && max == null ? _dash : '${tempText(min)} – ${tempText(max)}';

/// Lượng mưa một chữ số thập phân, dấu phẩy thập phân: `22,0 mm`.
String rainText(num? mm) =>
    mm == null ? _dash : '${mm.toStringAsFixed(1).replaceAll('.', ',')} mm';

String pctText(int? p) => p == null ? _dash : '$p%';

String windText(int? kmh, String dir) {
  if (kmh == null) return _dash;
  return dir.isEmpty ? '$kmh km/h' : '$kmh km/h $dir';
}

String _two(int n) => n.toString().padLeft(2, '0');

/// Giờ Việt Nam (GMT+7, không đổi giờ mùa hè).
DateTime toVn(DateTime t) => t.toUtc().add(const Duration(hours: 7));

/// `Cập nhật lúc hh:mm dd/mm` theo giờ Việt Nam (FN-TT-01/EX-01).
String updatedText(DateTime? at) {
  if (at == null) return 'Chưa cập nhật';
  final v = toVn(at);
  return 'Cập nhật lúc ${_two(v.hour)}:${_two(v.minute)} '
      '${_two(v.day)}/${_two(v.month)}';
}

/// `dd/mm (dd/mm ÂL)` — ngày dương kèm ngày âm (FN-TT-01/AC-05).
String dualDate(String isoDate, LunarDay lunar) {
  if (isoDate.length < 10) return isoDate;
  final d = '${isoDate.substring(8, 10)}/${isoDate.substring(5, 7)}';
  return lunar.label.isEmpty ? d : '$d (${lunar.label} ÂL)';
}

const _weekdays = [
  'Thứ Hai',
  'Thứ Ba',
  'Thứ Tư',
  'Thứ Năm',
  'Thứ Sáu',
  'Thứ Bảy',
  'Chủ Nhật',
];

/// `Thứ Hai, 23/11/2026`.
String longDate(String isoDate) {
  final d = DateTime.tryParse(isoDate);
  if (d == null) return isoDate;
  return '${_weekdays[d.weekday - 1]}, ${_two(d.day)}/${_two(d.month)}/${d.year}';
}

/// `YYYY-MM` của tháng lệch [delta] tháng so với [month].
String shiftMonth(String month, int delta) {
  final y = int.parse(month.substring(0, 4));
  final m = int.parse(month.substring(5, 7)) - 1 + delta;
  final ny = y + (m / 12).floor();
  final nm = m % 12 + 1;
  return '$ny-${_two(nm)}';
}

String monthOf(DateTime day) => '${day.year}-${_two(day.month)}';

/// `Tháng 11/2026`.
String monthTitle(String month) =>
    'Tháng ${month.substring(5, 7)}/${month.substring(0, 4)}';

Map<String, Object?> _map(Object? raw) =>
    raw is Map ? raw.cast<String, Object?>() : const {};

int? _int(Object? v) => v is num ? v.round() : null;

num? _num(Object? v) => v is num ? v : null;
