import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:flutter/material.dart';

import '../../application/household_season_service.dart';
import 'open_season_screen.dart' show parseDisplayDay;

String _iso(DateTime d) =>
    '${d.year.toString().padLeft(4, '0')}-${d.month.toString().padLeft(2, '0')}-${d.day.toString().padLeft(2, '0')}';
String _display(DateTime d) =>
    '${d.day.toString().padLeft(2, '0')}/${d.month.toString().padLeft(2, '0')}/${d.year}';

/// Ngày hôm nay theo giờ Việt Nam (GMT+7), dạng UTC 00:00.
DateTime vnToday() {
  final n = DateTime.now().toUtc().add(const Duration(hours: 7));
  return DateTime.utc(n.year, n.month, n.day);
}

/// AH-100: kiểm sớm phía app (máy chủ kiểm lại): tấn không âm, tối đa 2 chữ số lẻ;
/// mốc gốc ≤ ngày thu hoạch ≤ hôm nay.
String? harvestFormError(
  String mass,
  String day,
  String anchor,
  DateTime today,
) {
  if (mass.trim().isEmpty) return 'Nhập tổng sản lượng (tấn).';
  final m = tonnesError(mass);
  if (m != null) return m;
  final d = parseDisplayDay(day);
  if (d == null) return 'Ngày thu hoạch dạng dd/mm/yyyy.';
  if (_iso(d).compareTo(anchor) < 0 || d.isAfter(today)) {
    return 'Ngày thu hoạch từ mốc gốc (${anchor.split('-').reversed.join('/')}) đến hôm nay.';
  }
  return null;
}

bool _zero(String mass) => RegExp(r'^0+([.,]0+)?$').hasMatch(mass.trim());

/// AH-100 (FN-06.1): khai báo tổng sản lượng và ngày thu hoạch cuối của vụ đang canh
/// tác (cần mạng). Lưu xong vụ "Đã thu hoạch", việc Thu hoạch tự Xong. Sản lượng 0
/// phải xác nhận lại (KH-19). Hủy thì không lưu.
class HouseholdHarvestCard extends StatefulWidget {
  const HouseholdHarvestCard({
    super.key,
    required this.service,
    required this.coop,
    required this.season,
    required this.onDeclared,
  });
  final HouseholdSeasonService service;
  final String coop;
  final HouseholdJson season;
  final void Function(HouseholdJson result) onDeclared;
  @override
  State<HouseholdHarvestCard> createState() => _HouseholdHarvestCardState();
}

class _HouseholdHarvestCardState extends State<HouseholdHarvestCard> {
  final _ids = UuidV7Generator();
  final _mass = TextEditingController();
  final _day = TextEditingController();
  bool _open = false, _busy = false, _confirmZero = false;
  String? _error;
  Future<HouseholdJson>? _gps;
  (String, String)? _key;

  @override
  void dispose() {
    _mass.dispose();
    _day.dispose();
    super.dispose();
  }

  void _start() {
    _day.text = _display(vnToday());
    _mass.clear();
    // GPS một lần khi mở form (tối đa 2 giây), giữ nguyên khi thử lại.
    _gps = widget.service.gps().timeout(
      const Duration(seconds: 2),
      onTimeout: () => {'source': 'no_device'},
    );
    setState(() {
      _open = true;
      _confirmZero = false;
      _error = null;
      _key = null;
    });
  }

  Future<void> _save() async {
    if (_busy) return;
    final s = widget.season;
    final error = harvestFormError(
      _mass.text,
      _day.text,
      '${s['anchor_date']}',
      vnToday(),
    );
    setState(() => _error = error);
    if (error != null) return;
    final zero = _zero(_mass.text);
    if (zero && !_confirmZero) {
      setState(() => _confirmZero = true);
      return;
    }
    final day = _iso(parseDisplayDay(_day.text)!);
    final mass = tonnesValue(_mass.text)!;
    final fingerprint = '$mass|$day';
    if (_key?.$1 != fingerprint) _key = (fingerprint, _ids.newId());
    setState(() => _busy = true);
    try {
      final result = await widget.service.declareHarvest(
        widget.coop,
        '${s['household_ref']}',
        '${s['season_ref']}',
        expectedEventId: '${s['current_event_id']}',
        massT: mass,
        harvestDate: day,
        zeroConfirmed: zero,
        gps:
            await (_gps ??
                Future.value(<String, Object?>{'source': 'no_device'})),
        key: _key!.$2,
      );
      if (!mounted) return;
      setState(() {
        _open = false;
        _key = null;
      });
      widget.onDeclared(result);
    } catch (e) {
      if (!mounted) return;
      setState(
        () => _error = e is ApiException
            ? (e.detail ?? 'Không lưu được khai báo sản lượng.')
            : e is ArgumentError
            ? '${e.message}'
            : 'Chưa lưu được. Kiểm tra mạng rồi thử lại; dữ liệu được giữ.',
      );
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    if (!_open) {
      return FilledButton(
        onPressed: _start,
        child: const Text('Khai báo sản lượng'),
      );
    }
    final zero = _zero(_mass.text);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text(
          'Tổng sản lượng cả vụ trên ${widget.season['area_ha']} ha. Lưu xong vụ chuyển '
          '"Đã thu hoạch", việc Thu hoạch tự Xong theo ngày thu hoạch.',
        ),
        TextField(
          controller: _mass,
          enabled: !_busy,
          keyboardType: const TextInputType.numberWithOptions(decimal: true),
          decoration: const InputDecoration(labelText: 'Tổng sản lượng (tấn)'),
          onChanged: (_) => setState(() => _confirmZero = false),
        ),
        TextField(
          controller: _day,
          enabled: !_busy,
          decoration: const InputDecoration(
            labelText: 'Ngày thu hoạch cuối (dd/mm/yyyy)',
          ),
        ),
        if (zero && _confirmZero)
          const Text(
            'Sản lượng bằng 0. Bấm "Xác nhận lưu" nếu đúng là vụ không có thu hoạch.',
          ),
        if (_error != null)
          Text(
            _error!,
            style: TextStyle(color: Theme.of(context).colorScheme.error),
          ),
        Row(
          children: [
            FilledButton(
              onPressed: _busy ? null : _save,
              child: Text(
                _busy
                    ? 'Đang lưu…'
                    : zero && _confirmZero
                    ? 'Xác nhận lưu'
                    : 'Lưu khai báo',
              ),
            ),
            TextButton(
              onPressed: _busy
                  ? null
                  : () => setState(() {
                      _open = false;
                      _error = null;
                      _key = null;
                    }),
              child: const Text('Hủy'),
            ),
          ],
        ),
      ],
    );
  }
}
