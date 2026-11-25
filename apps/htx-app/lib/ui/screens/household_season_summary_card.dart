import 'dart:async';

import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:flutter/material.dart';

import '../../application/household_season_service.dart';
import '../../domain/season_summary.dart';

String _tons(Object? v) => v == null ? '—' : '${'$v'.replaceAll('.', ',')} tấn';
String _day(Object? raw) =>
    '$raw'.split('T').first.split('-').reversed.join('/');
String _num(Object? v) => '$v'.replaceAll('.', ',');

/// AH-152 (FN-06.5, FN-06-B4): "Ghi lại vụ" — năng suất, vật tư theo liều thực tế, số việc
/// sớm / đúng hạn / trễ / bỏ qua / chưa làm; nhắc khai báo sản lượng khi đã xong giai đoạn
/// cuối; Thành viên HTX hoàn thành vụ "Đã thu hoạch" (xác nhận hai bước, chi phí và ghi chú
/// tùy chọn, cần mạng). Vụ đã hoàn thành hiển thị bản chụp cố định. Đại diện HTX chỉ xem.
class HouseholdSeasonSummaryCard extends StatefulWidget {
  const HouseholdSeasonSummaryCard({
    super.key,
    required this.service,
    required this.coop,
    required this.season,
    required this.canWrite,
    required this.onClosed,
  });
  final HouseholdSeasonService service;
  final String coop;
  final HouseholdJson season;
  final bool canWrite;
  final void Function(HouseholdJson result) onClosed;
  @override
  State<HouseholdSeasonSummaryCard> createState() =>
      _HouseholdSeasonSummaryCardState();
}

class _HouseholdSeasonSummaryCardState
    extends State<HouseholdSeasonSummaryCard> {
  final _ids = UuidV7Generator();
  final _cost = TextEditingController();
  final _note = TextEditingController();
  HouseholdJson? _summary;
  bool _loading = true, _open = false, _confirm = false, _busy = false;
  String? _error, _loadError;
  Future<HouseholdJson>? _gps;
  (String, String)? _key;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void didUpdateWidget(HouseholdSeasonSummaryCard old) {
    super.didUpdateWidget(old);
    if (old.season['current_event_id'] != widget.season['current_event_id']) {
      _load();
    }
  }

  @override
  void dispose() {
    _cost.dispose();
    _note.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _loadError = null;
    });
    try {
      final s = widget.season;
      final v = await widget.service.seasonSummary(
        widget.coop,
        '${s['household_ref']}',
        '${s['season_ref']}',
      );
      if (mounted) setState(() => _summary = v);
    } catch (_) {
      if (mounted) setState(() => _loadError = 'Cần mạng để xem tổng kết vụ.');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  void _start() {
    _cost.clear();
    _note.clear();
    _gps = widget.service.gps().timeout(
      const Duration(seconds: 2),
      onTimeout: () => {'source': 'no_device'},
    );
    setState(() {
      _open = true;
      _confirm = false;
      _error = null;
      _key = null;
    });
  }

  Future<void> _save() async {
    if (_busy) return;
    final error = costError(_cost.text) ?? noteError(_note.text);
    setState(() => _error = error);
    if (error != null) return;
    if (!_confirm) {
      setState(() => _confirm = true);
      return;
    }
    final s = widget.season;
    final fingerprint = '${normalizeCost(_cost.text)}|${_note.text.trim()}';
    if (_key?.$1 != fingerprint) _key = (fingerprint, _ids.newId());
    setState(() => _busy = true);
    try {
      final result = await widget.service.closeSeason(
        widget.coop,
        '${s['household_ref']}',
        '${s['season_ref']}',
        expectedEventId: '${s['current_event_id']}',
        costVnd: _cost.text,
        note: _note.text,
        gps:
            await (_gps ??
                Future.value(<String, Object?>{'source': 'no_device'})),
        key: _key!.$2,
      );
      if (!mounted) return;
      setState(() {
        _open = false;
        _key = null;
        _summary = (result['summary'] as Map).cast<String, Object?>();
      });
      widget.onClosed(result);
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _confirm = false;
        _error = e is ApiException
            ? switch (e.code) {
                'cropcycle.season_harvest_required' => 'Chưa khai báo sản lượng: khai báo sản lượng trước khi hoàn thành vụ.',
                'cropcycle.season_last_stage_unresolved' =>
                  'Còn việc giai đoạn cuối chưa Xong hoặc Bỏ qua.',
                'cropcycle.task_season_changed' =>
                  'Vụ vừa thay đổi ở nơi khác. Quay lại và mở lại màn này.',
                _ => e.detail ?? 'Không lưu được.',
              }
            : e is ArgumentError
            ? '${e.message}'
            : 'Chưa lưu được. Kiểm tra mạng rồi thử lại; dữ liệu được giữ.';
      });
      if (e is ApiException && e.status == 409) unawaited(_load());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    if (widget.season['status'] == 'cancelled') return const SizedBox.shrink();
    if (_loading && _summary == null) {
      return const Padding(
        padding: EdgeInsets.all(8),
        child: Text('Đang tải tổng kết vụ…'),
      );
    }
    final v = _summary;
    if (v == null) return Text(_loadError ?? 'Chưa có tổng kết vụ.');
    final closed = v['status'] == 'closed';
    final tasks = (v['tasks'] as Map? ?? const {}).cast<String, Object?>();
    final materials = (v['materials'] as List? ?? const [])
        .whereType<Map<Object?, Object?>>()
        .toList();
    final harvest = widget.season['harvest'];
    final current = harvest is Map ? harvest['mass_t'] : null;
    final unresolved = (v['unresolved_steps'] as List? ?? const []);
    return Column(
      key: const ValueKey('season-summary'),
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text(
          closed ? 'Ghi lại vụ · Đã hoàn thành vụ' : 'Ghi lại vụ',
          style: Theme.of(context).textTheme.titleMedium,
        ),
        if (v['harvest_reminder'] == true)
          const Text(
            'Đã xong việc giai đoạn cuối. Khai báo sản lượng để hoàn thành vụ.',
            key: ValueKey('harvest-reminder'),
          ),
        ListTile(
          title: const Text('Năng suất'),
          subtitle: Text(
            v['yield_t_per_ha'] == null
                ? 'Chưa khai báo sản lượng'
                : '${_tons(v['mass_t'])} trên ${_num(v['area_ha'])} ha',
          ),
          trailing: Text(
            v['yield_t_per_ha'] == null
                ? '—'
                : '${_num(v['yield_t_per_ha'])} tấn/ha',
          ),
        ),
        if (closed && current != null && current != v['mass_t'])
          Text(
            'Sản lượng hiện hành ${_tons(current)} (đã sửa sau khi hoàn thành vụ; '
            'tổng kết giữ số lúc hoàn thành).',
          ),
        ListTile(
          title: const Text('Vật tư theo liều thực tế'),
          subtitle: Text(
            materials.isEmpty
                ? 'Chưa ghi vật tư'
                : materials
                      .map(
                        (m) => '${m['name']}: ${_num(m['qty'])} ${m['unit']}',
                      )
                      .join('\n'),
          ),
        ),
        ListTile(
          title: const Text('Việc theo quy trình'),
          subtitle: Text(taskLine(tasks)),
        ),
        if (closed) ...[
          ListTile(
            title: const Text('Chi phí'),
            trailing: Text(
              v['cost_vnd'] == null
                  ? 'Không nhập'
                  : '${groupVnd('${v['cost_vnd']}')} đ',
            ),
          ),
          ListTile(
            title: const Text('Ghi chú'),
            subtitle: Text('${v['note'] ?? 'Không có'}'),
          ),
          if (v['closed_at'] != null)
            ListTile(
              title: const Text('Hoàn thành vụ'),
              trailing: Text(_day(v['closed_at'])),
            ),
        ],
        if (!closed && v['status'] == 'completed' && unresolved.isNotEmpty)
          Text(
            'Còn việc giai đoạn cuối chưa Xong hoặc Bỏ qua '
            '(việc số ${unresolved.join(', ')}).',
          ),
        if (widget.canWrite && v['can_close'] == true && !_open)
          FilledButton(onPressed: _start, child: const Text('Hoàn thành vụ')),
        if (_open) ...[
          TextField(
            controller: _cost,
            enabled: !_busy,
            keyboardType: TextInputType.number,
            decoration: const InputDecoration(
              labelText: 'Chi phí cả vụ (đồng, không bắt buộc)',
            ),
            onChanged: (_) => setState(() => _confirm = false),
          ),
          TextField(
            controller: _note,
            enabled: !_busy,
            maxLength: 1000,
            decoration: const InputDecoration(
              labelText: 'Ghi chú (không bắt buộc)',
            ),
            onChanged: (_) => setState(() => _confirm = false),
          ),
          if (_confirm)
            const Text(
              'Vụ sẽ chuyển "Đã hoàn thành vụ" và không quay lại; không ghi thêm việc. '
              'Bấm "Xác nhận hoàn thành vụ" nếu đúng.',
            ),
        ],
        if (_error != null)
          Text(
            _error!,
            style: TextStyle(color: Theme.of(context).colorScheme.error),
          ),
        if (_open)
          Row(
            children: [
              FilledButton(
                onPressed: _busy ? null : _save,
                child: Text(
                  _busy
                      ? 'Đang lưu…'
                      : _confirm
                      ? 'Xác nhận hoàn thành vụ'
                      : 'Lưu hoàn thành vụ',
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
                child: const Text('Thôi'),
              ),
            ],
          ),
      ],
    );
  }
}
