import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/weather_service.dart';
import '../../domain/weather.dart';
import '../providers.dart';

WeatherService _service(Ref ref) => WeatherService(
  api: servicesOf(ref).api,
  db: servicesOf(ref).db,
  profile: servicesOf(ref).profile,
);

final weatherTodayProvider = FutureProvider.autoDispose<WeatherToday>(
  (ref) => _service(ref).today(),
);

final _calendarProvider = FutureProvider.autoDispose
    .family<WeatherCalendar, String>(
      (ref, month) => _service(ref).calendar(month),
    );

const _big = TextStyle(fontSize: 17);
const _muted = TextStyle(fontSize: 15, color: AgColors.mute);

/// Thẻ "Thời tiết hôm nay" trên Trang chủ (FN-TT-01, AH-225) — Đại diện HTX,
/// Thành viên HTX. Chạm thẻ → Lịch thời tiết.
class WeatherCard extends ConsumerWidget {
  const WeatherCard({super.key, this.onOpenCalendar});

  final VoidCallback? onOpenCalendar;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final w = ref.watch(weatherTodayProvider);
    return Card(
      key: const ValueKey('weather-card'),
      child: Semantics(
        label: 'Thời tiết hôm nay',
        container: true,
        child: InkWell(
          onTap: onOpenCalendar,
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: w.when(
              loading: () => const Row(
                children: [
                  SizedBox.square(
                    dimension: 24,
                    child: CircularProgressIndicator(strokeWidth: 3),
                  ),
                  SizedBox(width: 12),
                  Expanded(child: Text('Đang tải thời tiết…', style: _big)),
                ],
              ),
              error: (e, _) => _Header(
                icon: Icons.cloud_off_outlined,
                title: 'Thời tiết hôm nay',
                lines: ['$e'],
              ),
              data: (t) => _TodayBody(t),
            ),
          ),
        ),
      ),
    );
  }
}

class _Header extends StatelessWidget {
  const _Header({required this.icon, required this.title, required this.lines});
  final IconData icon;
  final String title;
  final List<String> lines;

  @override
  Widget build(BuildContext context) => Row(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Icon(icon, size: 36, color: AgColors.mute),
      const SizedBox(width: 12),
      Expanded(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              title,
              style: const TextStyle(fontSize: 18, fontWeight: FontWeight.w700),
            ),
            for (final l in lines) Text(l, style: _big),
          ],
        ),
      ),
    ],
  );
}

class _TodayBody extends StatelessWidget {
  const _TodayBody(this.t);
  final WeatherToday t;

  @override
  Widget build(BuildContext context) {
    final date = dualDate(t.date, t.lunar);
    if (!t.ok) {
      // EX-02, EX-03: vẫn hiện ngày dương, ngày âm kèm câu báo.
      return _Header(
        icon: Icons.cloud_off_outlined,
        title: 'Thời tiết hôm nay · $date',
        lines: [t.message],
      );
    }
    final c = t.current;
    final d = t.today;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          'Thời tiết hôm nay · $date',
          style: const TextStyle(fontSize: 18, fontWeight: FontWeight.w700),
        ),
        const SizedBox(height: 8),
        Row(
          children: [
            const Icon(
              Icons.wb_cloudy_outlined,
              size: 40,
              color: AgColors.blue,
            ),
            const SizedBox(width: 12),
            Text(
              tempText(c?.temperatureC),
              style: const TextStyle(fontSize: 32, fontWeight: FontWeight.w800),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Text(
                (c?.condition.isNotEmpty ?? false)
                    ? c!.condition
                    : (d?.condition ?? ''),
                style: _big,
              ),
            ),
          ],
        ),
        const SizedBox(height: 8),
        _Figure('Nhiệt độ', rangeText(d?.temperatureMinC, d?.temperatureMaxC)),
        _Figure('Lượng mưa', rainText(d?.precipitationMm)),
        _Figure('Khả năng mưa', pctText(d?.precipitationProbability)),
        _Figure(
          'Gió',
          windText(
            c?.windSpeedKmh ?? d?.windSpeedKmh,
            (c?.windDirection.isNotEmpty ?? false)
                ? c!.windDirection
                : (d?.windDirection ?? ''),
          ),
        ),
        _Figure('Độ ẩm', pctText(c?.humidityPct ?? d?.humidityPct)),
        const SizedBox(height: 4),
        Text(
          [
            if (t.source.label.isNotEmpty) 'Nguồn: ${t.source.label}',
            if (t.showUpdatedAt) updatedText(t.source.updatedAt),
          ].join(' · '),
          style: _muted,
        ),
      ],
    );
  }
}

class _Figure extends StatelessWidget {
  const _Figure(this.label, this.value);
  final String label;
  final String value;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.symmetric(vertical: 2),
    child: Row(
      children: [
        SizedBox(width: 130, child: Text(label, style: _muted)),
        Expanded(child: Text(value, style: _big)),
      ],
    ),
  );
}

/// Lịch thời tiết (FN-TT-02, AH-226): lịch tháng ngày dương kèm ngày âm, chạm
/// một ngày → chi tiết theo buổi.
class WeatherCalendarScreen extends ConsumerStatefulWidget {
  const WeatherCalendarScreen({super.key});

  @override
  ConsumerState<WeatherCalendarScreen> createState() =>
      _WeatherCalendarScreenState();
}

class _WeatherCalendarScreenState extends ConsumerState<WeatherCalendarScreen> {
  late String _today;
  late String _month;
  String? _selected;

  @override
  void initState() {
    super.initState();
    final now =
        ref.read(appControllerProvider).services?.crop.today() ??
        toVn(DateTime.now());
    _today = '${monthOf(now)}-${now.day.toString().padLeft(2, '0')}';
    _month = monthOf(now);
  }

  void _go(int delta) => setState(() {
    _month = shiftMonth(_month, delta);
    _selected = null;
  });

  @override
  Widget build(BuildContext context) {
    final cal = ref.watch(_calendarProvider(_month));
    return Scaffold(
      appBar: AppBar(title: const Text('Lịch thời tiết')),
      body: RefreshIndicator(
        onRefresh: () => ref.refresh(_calendarProvider(_month).future),
        child: ListView(
          padding: const EdgeInsets.all(12),
          children: [
            Row(
              children: [
                IconButton(
                  tooltip: 'Tháng trước',
                  iconSize: 32,
                  icon: const Icon(Icons.chevron_left),
                  onPressed: () => _go(-1),
                ),
                Expanded(
                  child: Text(
                    monthTitle(_month),
                    textAlign: TextAlign.center,
                    style: const TextStyle(
                      fontSize: 20,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ),
                IconButton(
                  tooltip: 'Tháng sau',
                  iconSize: 32,
                  icon: const Icon(Icons.chevron_right),
                  onPressed: () => _go(1),
                ),
              ],
            ),
            ...cal.when(
              loading: () => const [
                Padding(
                  padding: EdgeInsets.all(24),
                  child: Center(child: CircularProgressIndicator()),
                ),
              ],
              error: (e, _) => [
                Padding(
                  padding: const EdgeInsets.all(24),
                  child: Text('$e', style: _big),
                ),
              ],
              data: (c) => _content(c),
            ),
          ],
        ),
      ),
    );
  }

  List<Widget> _content(WeatherCalendar c) {
    final selected =
        _selected ??
        (c.days.any((d) => d.date == _today)
            ? _today
            : (c.days.isEmpty ? null : c.days.first.date));
    final day = c.days.where((d) => d.date == selected).firstOrNull;
    return [
      if (!c.fresh)
        Card(
          child: ListTile(
            leading: const Icon(Icons.cloud_off),
            title: Text(
              'Đang xem số liệu đã lưu (chưa có mạng). '
              '${updatedText(c.source.updatedAt)}',
            ),
          ),
        ),
      if (c.status != WeatherStatus.ok)
        Card(
          color: AgColors.amberBg,
          child: ListTile(
            leading: const Icon(Icons.info_outline, color: AgColors.amber),
            title: Text(c.message, style: _big),
          ),
        ),
      _MonthGrid(
        days: c.days,
        today: _today,
        selected: selected,
        onSelect: (d) => setState(() => _selected = d),
      ),
      const SizedBox(height: 12),
      if (day != null) _DayDetail(day),
      if (c.source.label.isNotEmpty)
        Padding(
          padding: const EdgeInsets.only(top: 8),
          child: Text(
            [
              'Nguồn: ${c.source.label}',
              if (c.source.stale) updatedText(c.source.updatedAt),
            ].join(' · '),
            style: _muted,
          ),
        ),
    ];
  }
}

const _weekdayShort = ['T2', 'T3', 'T4', 'T5', 'T6', 'T7', 'CN'];

class _MonthGrid extends StatelessWidget {
  const _MonthGrid({
    required this.days,
    required this.today,
    required this.selected,
    required this.onSelect,
  });

  final List<CalendarDay> days;
  final String today;
  final String? selected;
  final ValueChanged<String> onSelect;

  @override
  Widget build(BuildContext context) {
    final first = days.isEmpty ? null : DateTime.tryParse(days.first.date);
    final lead = first == null ? 0 : first.weekday - 1;
    return Column(
      children: [
        Row(
          children: [
            for (final w in _weekdayShort)
              Expanded(
                child: Text(
                  w,
                  textAlign: TextAlign.center,
                  style: const TextStyle(
                    fontWeight: FontWeight.w700,
                    fontSize: 15,
                  ),
                ),
              ),
          ],
        ),
        const SizedBox(height: 4),
        GridView(
          shrinkWrap: true,
          physics: const NeverScrollableScrollPhysics(),
          gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
            crossAxisCount: 7,
            mainAxisExtent: 76,
            mainAxisSpacing: 2,
            crossAxisSpacing: 2,
          ),
          children: [
            for (var i = 0; i < lead; i++) const SizedBox.shrink(),
            for (final d in days)
              _DayCell(
                day: d,
                today: d.date == today,
                selected: d.date == selected,
                onTap: () => onSelect(d.date),
              ),
          ],
        ),
      ],
    );
  }
}

class _DayCell extends StatelessWidget {
  const _DayCell({
    required this.day,
    required this.today,
    required this.selected,
    required this.onTap,
  });

  final CalendarDay day;
  final bool today;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final w = day.weather;
    // Mùng 1 hiện cả tháng âm; ngày khác chỉ hiện ngày âm.
    final lunar = day.lunar.firstDay ? day.lunar.label : '${day.lunar.day}';
    final temp = w?.summary.temperatureMaxC;
    return Semantics(
      button: true,
      selected: selected,
      label:
          '${longDate(day.date)}, âm lịch ${day.lunar.label}'
          '${w == null ? '' : ', ${w.summary.condition}'}',
      excludeSemantics: true,
      child: Material(
        color: selected
            ? AgColors.g100
            : (today ? AgColors.amberBg : Colors.white),
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(6),
          side: BorderSide(
            color: selected ? AgColors.g700 : const Color(0xFFE5E7EB),
            width: selected ? 2 : 1,
          ),
        ),
        child: InkWell(
          key: ValueKey('day-${day.date}'),
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.all(2),
            // Cỡ chữ lớn (người dùng phóng to) → co lại trong ô, không tràn.
            child: FittedBox(
              fit: BoxFit.scaleDown,
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(
                    '${day.day}',
                    style: const TextStyle(
                      fontSize: 17,
                      fontWeight: FontWeight.w800,
                    ),
                  ),
                  Text(
                    lunar,
                    style: TextStyle(
                      fontSize: 12,
                      color: day.lunar.firstDay || day.lunar.fullMoon
                          ? AgColors.red
                          : AgColors.mute,
                    ),
                  ),
                  Text(
                    temp == null ? '' : '$temp°',
                    style: const TextStyle(fontSize: 13, color: AgColors.blue),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class _DayDetail extends StatelessWidget {
  const _DayDetail(this.day);
  final CalendarDay day;

  @override
  Widget build(BuildContext context) {
    final w = day.weather;
    return Card(
      key: const ValueKey('day-detail'),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              longDate(day.date),
              style: const TextStyle(fontSize: 18, fontWeight: FontWeight.w700),
            ),
            Text('Âm lịch: ${day.lunar.label}', style: _muted),
            const SizedBox(height: 8),
            if (w == null)
              const Text(msgNoData, style: _big)
            else ...[
              if (w.label.isNotEmpty)
                Padding(
                  padding: const EdgeInsets.only(bottom: 4),
                  child: Chip(label: Text(w.label)),
                ),
              Text(w.summary.condition, style: _big),
              _Figure(
                'Nhiệt độ',
                rangeText(w.summary.temperatureMinC, w.summary.temperatureMaxC),
              ),
              _Figure('Lượng mưa', rainText(w.summary.precipitationMm)),
              _Figure(
                'Khả năng mưa',
                pctText(w.summary.precipitationProbability),
              ),
              _Figure(
                'Gió',
                windText(w.summary.windSpeedKmh, w.summary.windDirection),
              ),
              const Divider(),
              if (w.hourly && w.periods.isNotEmpty)
                for (final p in w.periods) _PeriodRow(p)
              else
                // EX-01: không có số liệu theo giờ → một khối cả ngày.
                _PeriodRow(WeatherPeriod('Cả ngày', w.summary)),
            ],
          ],
        ),
      ),
    );
  }
}

class _PeriodRow extends StatelessWidget {
  const _PeriodRow(this.p);
  final WeatherPeriod p;

  @override
  Widget build(BuildContext context) {
    final f = p.figures;
    final temp = f.temperatureC != null
        ? tempText(f.temperatureC)
        : rangeText(f.temperatureMinC, f.temperatureMaxC);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 4),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 70,
            child: Text(
              p.name,
              style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w700),
            ),
          ),
          Expanded(
            child: Text(
              [
                if (f.condition.isNotEmpty) f.condition,
                temp,
                'mưa ${rainText(f.precipitationMm)}',
                pctText(f.precipitationProbability),
                windText(f.windSpeedKmh, ''),
              ].join(' · '),
              style: const TextStyle(fontSize: 16),
            ),
          ),
        ],
      ),
    );
  }
}
