import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
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
/// tác. Lưu xong vụ "Đã thu hoạch", việc Thu hoạch tự Xong. Sản lượng 0 phải xác nhận
/// lại (KH-19). Hủy thì không lưu. AH-190: mất mạng thì lưu trên máy "Chờ đồng bộ"
/// (hàng đợi đồng bộ), tự gửi khi có mạng; [initialMass], [initialDay] điền sẵn khi
/// khai lại bản bị từ chối.
class HouseholdHarvestCard extends StatefulWidget {
  const HouseholdHarvestCard({
    super.key,
    required this.service,
    required this.coop,
    required this.season,
    required this.onDeclared,
    this.onQueued,
    this.initialMass,
    this.initialDay,
  });
  final HouseholdSeasonService service;
  final String coop;
  final HouseholdJson season;
  final void Function(HouseholdJson result) onDeclared;
  final void Function(OutboxRecord record)? onQueued;
  final String? initialMass;
  final String? initialDay;
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

  @override
  void initState() {
    super.initState();
    // AH-190: khai lại bản bị từ chối — mở sẵn form với số đã nhập.
    if (widget.initialMass != null) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) _start();
      });
    }
  }

  void _start() {
    _day.text = widget.initialDay ?? _display(vnToday());
    _mass.text = widget.initialMass ?? '';
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
    } on NetworkException {
      if (!mounted) return;
      await _queue(day, mass, zero);
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

  /// AH-190: lưu bản khai trên máy (hàng đợi đồng bộ) khi không gửi được vì mạng.
  Future<void> _queue(String day, String mass, bool zero) async {
    final s = widget.season;
    if (!widget.service.canQueueOffline || widget.onQueued == null) {
      setState(
        () => _error =
            'Chưa lưu được. Kiểm tra mạng rồi thử lại; dữ liệu được giữ.',
      );
      return;
    }
    try {
      final record = await widget.service.queueHarvest(
        widget.coop,
        '${s['household_ref']}',
        '${s['season_ref']}',
        massT: mass,
        harvestDate: day,
        zeroConfirmed: zero,
        gps:
            await (_gps ??
                Future.value(<String, Object?>{'source': 'no_device'})),
      );
      if (!mounted) return;
      setState(() {
        _open = false;
        _key = null;
      });
      widget.onQueued!(record);
    } catch (e) {
      if (!mounted) return;
      setState(
        () => _error = e is StateError
            ? e.message
            : 'Không lưu được trên máy. Thử lại.',
      );
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

/// AH-189 (FN-06.3): sửa sản lượng, ngày thu hoạch của vụ "Đã thu hoạch" (cần mạng). Lý
/// do tùy chọn; sản lượng 0 phải xác nhận lại. Hủy khai báo chỉ có trên web.
class HouseholdHarvestEditCard extends StatefulWidget {
  const HouseholdHarvestEditCard({
    super.key,
    required this.service,
    required this.coop,
    required this.season,
    required this.onSaved,
  });
  final HouseholdSeasonService service;
  final String coop;
  final HouseholdJson season;
  final void Function(HouseholdJson result) onSaved;
  @override
  State<HouseholdHarvestEditCard> createState() =>
      _HouseholdHarvestEditCardState();
}

class _HouseholdHarvestEditCardState extends State<HouseholdHarvestEditCard> {
  final _ids = UuidV7Generator();
  final _mass = TextEditingController();
  final _day = TextEditingController();
  final _reason = TextEditingController();
  bool _open = false, _busy = false, _confirmZero = false;
  String? _error;
  (String, String)? _key;

  HouseholdJson get _harvest =>
      (widget.season['harvest'] as Map).cast<String, Object?>();

  @override
  void dispose() {
    _mass.dispose();
    _day.dispose();
    _reason.dispose();
    super.dispose();
  }

  void _start() {
    _mass.text = '${_harvest['mass_t']}'.replaceAll('.', ',');
    _day.text = '${_harvest['harvest_date']}'.split('-').reversed.join('/');
    _reason.clear();
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
    var error = harvestFormError(
      _mass.text,
      _day.text,
      '${s['anchor_date']}',
      vnToday(),
    );
    final reason = _reason.text.trim();
    // AH-152: vụ "Đã hoàn thành vụ" bắt buộc lý do sửa.
    final closed = s['status'] == 'closed';
    if (error == null &&
        (reason.isNotEmpty || closed) &&
        (reason.length < 10 || reason.length > 500)) {
      error = closed
          ? 'Vụ đã hoàn thành: nhập lý do sửa từ 10 đến 500 ký tự.'
          : 'Lý do sửa từ 10 đến 500 ký tự (hoặc để trống).';
    }
    setState(() => _error = error);
    if (error != null) return;
    final zero = _zero(_mass.text);
    if (zero && !_confirmZero) {
      setState(() => _confirmZero = true);
      return;
    }
    final day = _iso(parseDisplayDay(_day.text)!);
    final mass = tonnesValue(_mass.text)!;
    final fingerprint = '$mass|$day|$reason';
    if (_key?.$1 != fingerprint) _key = (fingerprint, _ids.newId());
    setState(() => _busy = true);
    try {
      final result = await widget.service.correctHarvest(
        widget.coop,
        '${s['household_ref']}',
        '${s['season_ref']}',
        expectedHarvestId: '${_harvest['harvest_id']}',
        massT: mass,
        harvestDate: day,
        zeroConfirmed: zero,
        reason: reason,
        key: _key!.$2,
      );
      if (!mounted) return;
      setState(() {
        _open = false;
        _key = null;
      });
      widget.onSaved(result);
    } catch (e) {
      if (!mounted) return;
      setState(
        () => _error = e is ApiException
            ? (e.code == 'cropcycle.concurrent_edit'
                  ? 'Khai báo vừa được sửa ở nơi khác. Quay lại và mở lại màn này.'
                  : e.detail ?? 'Không lưu được.')
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
      return OutlinedButton(
        onPressed: _start,
        child: const Text('Sửa sản lượng'),
      );
    }
    final zero = _zero(_mass.text);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
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
        TextField(
          controller: _reason,
          enabled: !_busy,
          decoration: InputDecoration(
            labelText: widget.season['status'] == 'closed'
                ? 'Lý do sửa (bắt buộc)'
                : 'Lý do sửa (không bắt buộc)',
          ),
        ),
        if (zero && _confirmZero)
          const Text('Sản lượng bằng 0. Bấm "Xác nhận lưu" nếu đúng.'),
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
                    : 'Lưu sửa',
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

/// AH-189: lịch sử khai báo (khai báo, sửa, hủy) — ai xem được vụ đều xem được.
class HouseholdHarvestHistory extends StatelessWidget {
  const HouseholdHarvestHistory({super.key, required this.items});
  final List<HouseholdJson> items;
  static const _kind = {
    'declared': 'Khai báo',
    'corrected': 'Sửa',
    'voided': 'Hủy khai báo',
  };
  @override
  Widget build(BuildContext context) {
    String t(Object? v) => '${'$v'.replaceAll('.', ',')} tấn';
    String d(Object? v) => '$v'.split('-').reversed.join('/');
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        for (final v in items)
          ListTile(
            dense: true,
            title: Text(
              '${_kind[v['kind']] ?? v['kind']}: ${t(v['mass_t'])} · ngày ${d(v['harvest_date'])}',
            ),
            subtitle: v['reason'] == null
                ? null
                : Text('Lý do: ${v['reason']}'),
          ),
      ],
    );
  }
}

/// AH-190: lý do máy chủ từ chối bản khai ngoại tuyến → lời dễ hiểu.
String harvestRejectMessage(String? code) => switch (code) {
  'cropcycle.season_state' => 'Chưa đồng bộ: vụ không còn "Đang canh tác" (đã khai báo, dừng hoặc hoàn thành trong lúc mất mạng).',
  'cropcycle.harvest_zero_confirmation_required' =>
    'Chưa đồng bộ: sản lượng bằng 0 cần xác nhận lại.',
  'cropcycle.season_not_found' || 'cropcycle.household_not_found' =>
    'Chưa đồng bộ: vụ hoặc nông hộ không thuộc phạm vi của bạn.',
  'cropcycle.read_only' ||
  'forbidden' => 'Chưa đồng bộ: tài khoản không được khai báo sản lượng.',
  'validation_failed' => 'Chưa đồng bộ: số hoặc ngày chưa hợp lệ (ngày phải từ mốc gốc tới hôm nay).',
  null => 'Chưa đồng bộ: bị từ chối.',
  _ => 'Chưa đồng bộ: bị từ chối ($code).',
};

/// AH-190: bản khai sản lượng còn trên máy — "Chờ đồng bộ" hoặc bị từ chối kèm lý do;
/// bản bị từ chối xóa được hoặc khai lại (khi vụ còn "Đang canh tác").
class HouseholdHarvestPending extends StatelessWidget {
  const HouseholdHarvestPending({
    super.key,
    required this.record,
    required this.canRedo,
    required this.onDiscard,
    required this.onRedo,
  });
  final OutboxRecord record;
  final bool canRedo;
  final VoidCallback onDiscard;
  final VoidCallback onRedo;

  @override
  Widget build(BuildContext context) {
    final p = record.payload;
    final summary =
        '${'${p['mass_t']}'.replaceAll('.', ',')} tấn · thu hoạch ${'${p['harvest_date']}'.split('-').reversed.join('/')}';
    final rejected = record.status == OutboxStatus.rejected;
    return Card(
      key: const ValueKey('harvest-offline'),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(
              rejected
                  ? 'Bản khai trên máy — Không đồng bộ được'
                  : 'Đã lưu trên máy — Chờ đồng bộ',
              style: Theme.of(context).textTheme.titleSmall,
            ),
            Text(summary),
            if (rejected)
              Text(
                harvestRejectMessage(record.errorCode),
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              )
            else
              const Text('Tự gửi khi có mạng; không cần khai lại.'),
            if (rejected)
              Row(
                children: [
                  TextButton(
                    onPressed: onDiscard,
                    child: const Text('Xóa bản khai trên máy'),
                  ),
                  if (canRedo)
                    TextButton(
                      onPressed: onRedo,
                      child: const Text('Khai lại'),
                    ),
                ],
              ),
          ],
        ),
      ),
    );
  }
}
