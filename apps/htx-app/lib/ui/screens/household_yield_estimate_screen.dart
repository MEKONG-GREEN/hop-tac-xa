import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:agrigreen_mobile/domain/outbox_record.dart';
import 'package:flutter/material.dart';

import '../../application/household_season_service.dart';
import 'household_harvest_card.dart';
import 'household_season_summary_card.dart';

String _tons(Object? v) => v == null ? '—' : '${'$v'.replaceAll('.', ',')} tấn';
String _day(Object? raw) => '$raw'.split('-').reversed.join('/');

/// AH-87 (FN-04.2, FN-04.3, KH-05): sản lượng lúa, rơm dự kiến và ngày thu hoạch dự
/// kiến của vụ theo hộ. Thành viên HTX nhập giá trị của mình khi vụ đang canh tác (cần
/// mạng); Đại diện HTX chỉ xem. Ô trống = dùng giá trị hệ thống tính.
class HouseholdYieldEstimateScreen extends StatefulWidget {
  const HouseholdYieldEstimateScreen({
    super.key,
    required this.service,
    required this.coop,
    required this.season,
    required this.canWrite,
  });
  final HouseholdSeasonService service;
  final String coop;
  final HouseholdJson season;
  final bool canWrite;
  @override
  State<HouseholdYieldEstimateScreen> createState() =>
      _HouseholdYieldEstimateScreenState();
}

class _HouseholdYieldEstimateScreenState
    extends State<HouseholdYieldEstimateScreen> {
  final _ids = UuidV7Generator();
  final _rice = TextEditingController();
  final _straw = TextEditingController();
  HouseholdJson? _estimate;
  late HouseholdJson _season;
  List<HouseholdJson>? _history;

  Future<void> _loadHistory() async {
    try {
      final items = await widget.service.harvestHistory(
        widget.coop,
        '${_season['household_ref']}',
        '${_season['season_ref']}',
      );
      if (mounted) setState(() => _history = items);
    } catch (_) {
      if (mounted) {
        setState(() => _error = 'Cần mạng để xem lịch sử khai báo.');
      }
    }
  }

  bool _editing = false, _busy = false;
  String? _error, _riceError, _strawError;
  (String, String)? _key;
  // AH-190: bản khai sản lượng còn trên máy; số điền sẵn khi khai lại.
  OutboxRecord? _offline;
  (String, String)? _redo;

  @override
  void initState() {
    super.initState();
    _season = widget.season;
    final e = widget.season['yield_estimate'];
    _estimate = e is Map ? e.cast<String, Object?>() : null;
    _loadOffline();
  }

  Future<void> _loadOffline() async {
    final r = await widget.service.pendingHarvest(
      widget.coop,
      '${_season['household_ref']}',
      '${_season['season_ref']}',
    );
    if (mounted) setState(() => _offline = r);
  }

  Future<void> _discardOffline() async {
    final r = _offline;
    if (r == null) return;
    await widget.service.discardHarvest(r.id);
    if (mounted) setState(() => _offline = null);
  }

  Future<void> _redoOffline() async {
    final r = _offline;
    if (r == null) return;
    await widget.service.discardHarvest(r.id);
    if (!mounted) return;
    setState(() {
      _redo = (
        '${r.payload['mass_t']}'.replaceAll('.', ','),
        '${r.payload['harvest_date']}'.split('-').reversed.join('/'),
      );
      _offline = null;
    });
  }

  @override
  void dispose() {
    _rice.dispose();
    _straw.dispose();
    super.dispose();
  }

  bool get _isRice => _estimate?['is_rice'] == true;

  void _start() {
    _rice.text = '${_estimate?['entered_rice_t'] ?? ''}';
    _straw.text = '${_estimate?['entered_straw_t'] ?? ''}';
    setState(() {
      _editing = true;
      _error = _riceError = _strawError = null;
    });
  }

  Future<void> _save() async {
    final e = _estimate;
    if (e == null || _busy) return;
    setState(() {
      _riceError = tonnesError(_rice.text);
      _strawError = _isRice ? tonnesError(_straw.text) : null;
      _error = null;
    });
    if (_riceError != null || _strawError != null) return;
    final rice = tonnesValue(_rice.text);
    final straw = _isRice ? tonnesValue(_straw.text) : null;
    final fingerprint = '${e['seq']}|$rice|$straw';
    // Một Idempotency-Key cho mỗi nội dung gửi; thử lại cùng nội dung dùng lại khóa.
    if (_key?.$1 != fingerprint) _key = (fingerprint, _ids.newId());
    setState(() => _busy = true);
    try {
      final saved = await widget.service.saveYieldEstimate(
        widget.coop,
        '${widget.season['household_ref']}',
        '${widget.season['season_ref']}',
        expectedSeq: (e['seq'] as num).toInt(),
        riceT: rice,
        strawT: straw,
        key: _key!.$2,
      );
      if (!mounted) return;
      setState(() {
        _estimate = saved;
        _editing = false;
        _key = null;
      });
    } catch (err) {
      if (!mounted) return;
      setState(
        () => _error = err is ApiException
            ? (err.code == 'cropcycle.yield_estimate_changed'
                  ? 'Sản lượng dự kiến vừa thay đổi. Quay lại và mở lại màn này.'
                  : err.detail ?? 'Không lưu được sản lượng dự kiến.')
            : err is ArgumentError
            ? '${err.message}'
            : 'Chưa lưu được. Kiểm tra mạng rồi thử lại; dữ liệu được giữ.',
      );
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final e = _estimate;
    final s = _season;
    final harvest = s['expected_harvest_date'];
    final canEdit = widget.canWrite && s['status'] == 'open' && e != null;
    final actual = s['harvest'] is Map
        ? (s['harvest'] as Map).cast<String, Object?>()
        : null;
    return Scaffold(
      appBar: AppBar(title: const Text('Sản lượng')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          Text('${s['season_code']} ${s['season_year']} · ${s['crop_code']}'),
          const SizedBox(height: 8),
          if (e == null)
            const Text(
              'Chưa tính được sản lượng dự kiến (không đọc được tham số). Thử tải lại sau.',
            )
          else ...[
            ListTile(
              title: const Text('Lúa dự kiến'),
              subtitle: Text(
                e['entered_rice_t'] != null
                    ? 'Thành viên nhập · hệ thống tính ${_tons(e['computed_rice_t'])}'
                    : 'Hệ thống tính: ${e['area_ha']} ha × ${e['conventional_yield_t_per_ha']} tấn/ha',
              ),
              trailing: Text(_tons(e['rice_t'])),
            ),
            if (_isRice)
              ListTile(
                title: const Text('Rơm dự kiến'),
                subtitle: Text(
                  e['entered_straw_t'] != null
                      ? 'Thành viên nhập · hệ thống tính ${_tons(e['computed_straw_t'])}'
                      : 'Hệ thống tính, hệ số rơm ${e['straw_pct']} %',
                ),
                trailing: Text(_tons(e['straw_t'])),
              ),
          ],
          ListTile(
            title: const Text('Ngày thu hoạch dự kiến'),
            trailing: Text(harvest == null ? 'Chưa có' : _day(harvest)),
          ),
          // AH-100 AC2: sản lượng thực tế cạnh dự kiến.
          if (actual != null)
            ListTile(
              title: const Text('Sản lượng thực tế'),
              subtitle: Text(
                'Thu hoạch ${_day(actual['harvest_date'])} · năng suất '
                '${'${actual['yield_t_per_ha']}'.replaceAll('.', ',')} tấn/ha',
              ),
              trailing: Text(_tons(actual['mass_t'])),
            ),
          // AH-189: sửa sản lượng (Thành viên) và lịch sử khai báo (mọi vai xem được vụ).
          if (widget.canWrite &&
              (s['status'] == 'completed' || s['status'] == 'closed') &&
              actual != null)
            HouseholdHarvestEditCard(
              key: ValueKey('edit:${actual['harvest_id']}'),
              service: widget.service,
              coop: widget.coop,
              season: s,
              onSaved: (result) => setState(() {
                _season = (result['season'] as Map).cast<String, Object?>();
                _history = null;
              }),
            ),
          if (actual != null)
            _history == null
                ? TextButton(
                    onPressed: _loadHistory,
                    child: const Text('Xem lịch sử khai báo'),
                  )
                : HouseholdHarvestHistory(items: _history!),
          // AH-152: "Ghi lại vụ" — tổng kết, nhắc khai báo, hoàn thành vụ.
          HouseholdSeasonSummaryCard(
            key: ValueKey('summary:${s['season_ref']}'),
            service: widget.service,
            coop: widget.coop,
            season: s,
            canWrite: widget.canWrite,
            onClosed: (result) => setState(
              () => _season = (result['season'] as Map).cast<String, Object?>(),
            ),
          ),
          if (widget.canWrite && _offline != null)
            HouseholdHarvestPending(
              record: _offline!,
              canRedo: s['status'] == 'open',
              onDiscard: _discardOffline,
              onRedo: _redoOffline,
            ),
          if (widget.canWrite &&
              s['status'] == 'open' &&
              !_editing &&
              _offline == null)
            HouseholdHarvestCard(
              key: ValueKey('harvest:${_redo?.$1}:${_redo?.$2}'),
              service: widget.service,
              coop: widget.coop,
              season: s,
              initialMass: _redo?.$1,
              initialDay: _redo?.$2,
              onQueued: (record) => setState(() {
                _offline = record;
                _redo = null;
              }),
              onDeclared: (result) => setState(() {
                final season = (result['season'] as Map)
                    .cast<String, Object?>();
                _season = season;
                final e = season['yield_estimate'];
                if (e is Map) _estimate = e.cast<String, Object?>();
              }),
            ),
          if (canEdit && !_editing)
            OutlinedButton(
              onPressed: _start,
              child: const Text('Sửa sản lượng dự kiến'),
            ),
          if (canEdit && _editing) ...[
            const Text('Để trống để dùng giá trị hệ thống tính.'),
            TextField(
              controller: _rice,
              enabled: !_busy,
              keyboardType: const TextInputType.numberWithOptions(
                decimal: true,
              ),
              decoration: InputDecoration(
                labelText: 'Lúa dự kiến (tấn)',
                hintText: '${e['computed_rice_t']}',
                errorText: _riceError,
              ),
            ),
            if (_isRice)
              TextField(
                controller: _straw,
                enabled: !_busy,
                keyboardType: const TextInputType.numberWithOptions(
                  decimal: true,
                ),
                decoration: InputDecoration(
                  labelText: 'Rơm dự kiến (tấn)',
                  hintText: '${e['computed_straw_t'] ?? ''}',
                  errorText: _strawError,
                ),
              ),
            Row(
              children: [
                FilledButton(
                  onPressed: _busy ? null : _save,
                  child: Text(_busy ? 'Đang lưu…' : 'Lưu'),
                ),
                TextButton(
                  onPressed: _busy
                      ? null
                      : () => setState(() {
                          _editing = false;
                          _error = null;
                          _key = null;
                        }),
                  child: const Text('Hủy'),
                ),
              ],
            ),
          ],
          if (_error != null)
            Text(
              _error!,
              style: TextStyle(color: Theme.of(context).colorScheme.error),
            ),
        ],
      ),
    );
  }
}
