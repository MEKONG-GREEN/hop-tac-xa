import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:flutter/material.dart';

import '../../application/household_season_service.dart';

const _labels = {
  'upcoming': 'Chưa đến hạn',
  'due': 'Đến hạn',
  'overdue': 'Trễ hạn',
  'waiting_anchor': 'Chờ mốc',
  'done': 'Đã xong',
  'done_early': 'Xong sớm',
  'done_on_time': 'Xong đúng hạn',
  'done_late': 'Xong trễ',
  'skipped': 'Bỏ qua',
  'not_started': 'Chưa thực hiện',
  'in_progress': 'Đang thực hiện',
};
String _day(Object? raw) => '$raw'.split('-').reversed.join('/');
List<HouseholdJson> _rows(Object? raw) => (raw as List<Object?>)
    .map((v) => Map<String, Object?>.from(v as Map))
    .toList();

class _PendingTask {
  const _PendingTask(this.seq, this.name, this.event, this.key, this.gps);
  final int seq;
  final String name, event, key;
  final Future<HouseholdJson> gps;
}

class HouseholdChecklistScreen extends StatefulWidget {
  const HouseholdChecklistScreen({
    super.key,
    required this.service,
    required this.coop,
    required this.household,
    required this.season,
    required this.canWrite,
  });
  final HouseholdSeasonService service;
  final String coop, household;
  final HouseholdJson season;
  final bool canWrite;
  @override
  State<HouseholdChecklistScreen> createState() =>
      _HouseholdChecklistScreenState();
}

class _HouseholdChecklistScreenState extends State<HouseholdChecklistScreen> {
  final _ids = UuidV7Generator();
  late Future<HouseholdJson> _load;
  HouseholdJson? _updated;
  _PendingTask? _pending;
  bool _busy = false;
  String? _error;
  String get _season => widget.season['season_ref'] as String;
  @override
  void initState() {
    super.initState();
    _load = widget.service.checklist(widget.coop, widget.household, _season);
  }

  void _reload() {
    setState(() {
      _updated = null;
      _pending = null;
      _load = widget.service.checklist(widget.coop, widget.household, _season);
    });
  }

  void _select(HouseholdJson task, HouseholdJson data) {
    setState(() {
      _error = null;
      _pending = _pending?.seq == task['seq']
          ? null
          : _PendingTask(
              task['seq'] as int,
              task['name'] as String,
              data['current_event_id'] as String,
              _ids.newId(),
              widget.service.gps().timeout(
                const Duration(seconds: 2),
                onTimeout: () => {'source': 'no_device'},
              ),
            );
    });
  }

  Future<void> _confirm() async {
    final pending = _pending;
    if (pending == null || _busy) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final gps = await pending.gps;
      if (!mounted) return;
      final result = await widget.service.completeTask(
        widget.coop,
        widget.household,
        _season,
        {
          'step_seq': pending.seq,
          'expected_season_event_id': pending.event,
          'gps': gps,
        },
        pending.key,
      );
      if (!mounted) return;
      setState(() {
        _updated = Map<String, Object?>.from(result['checklist'] as Map);
        _pending = null;
        _load = widget.service.checklist(
          widget.coop,
          widget.household,
          _season,
        );
      });
    } catch (e) {
      if (!mounted) return;
      if (e is ApiException &&
          ((e.status == 409 && e.code != 'idempotency_in_progress') ||
              e.code == 'cropcycle.task_season_state' ||
              e.code == 'cropcycle.task_stage_locked')) {
        _reload();
      }
      setState(
        () => _error = e is ApiException
            ? (e.detail ?? 'Không thể ghi việc. Kiểm tra lại trạng thái vụ.')
            : 'Chưa ghi được. Kiểm tra mạng rồi xác nhận lại; dữ liệu được giữ.',
      );
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text(
          '${widget.season['season_code']} ${widget.season['season_year']}',
        ),
      ),
      body: FutureBuilder<HouseholdJson>(
        future: _load,
        builder: (context, state) {
          final data =
              (state.connectionState == ConnectionState.done && !state.hasError
                  ? state.data
                  : null) ??
              _updated;
          if (data == null) {
            if (state.hasError) {
              return Center(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    _header(),
                    Text(
                      state.error is ApiException
                          ? ((state.error as ApiException).detail ??
                                'Chưa tải được checklist của vụ.')
                          : 'Cần mạng để tải checklist.',
                    ),
                    TextButton(
                      onPressed: _reload,
                      child: const Text('Thử lại'),
                    ),
                  ],
                ),
              );
            }
            return const Center(child: CircularProgressIndicator());
          }
          final progress = Map<String, Object?>.from(data['progress'] as Map);
          final records = _rows(data['records']);
          return SingleChildScrollView(
            padding: const EdgeInsets.all(16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                _header(),
                Text(
                  'Tiến độ: ${progress['percent'] == null ? '—' : '${progress['percent']}%'}',
                ),
                if (_error != null)
                  Text(
                    _error!,
                    style: TextStyle(
                      color: Theme.of(context).colorScheme.error,
                    ),
                  ),
                for (final stage in _rows(progress['stages'])) ...[
                  Text(
                    '${stage['name']} · ${_labels[stage['status']]}',
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                  for (final task in _rows(stage['tasks']))
                    _task(task, data, records),
                ],
                TextButton(
                  onPressed: _busy ? null : _reload,
                  child: const Text('Tải lại checklist'),
                ),
              ],
            ),
          );
        },
      ),
      bottomNavigationBar: _pending == null
          ? null
          : SafeArea(
              child: Material(
                elevation: 8,
                child: Padding(
                  padding: const EdgeInsets.all(12),
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Text(
                        '${_pending!.name} · Ngày làm xong: Hôm nay (giờ Việt Nam)',
                      ),
                      Row(
                        children: [
                          Expanded(
                            child: TextButton(
                              onPressed: _busy
                                  ? null
                                  : () => setState(() => _pending = null),
                              child: const Text('Hủy'),
                            ),
                          ),
                          Expanded(
                            child: FilledButton(
                              onPressed: _busy ? null : _confirm,
                              child: Text(_busy ? 'Đang ghi…' : 'Xác nhận'),
                            ),
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
              ),
            ),
    );
  }

  Widget _header() {
    final snapshot = widget.season['sop_snapshot'] as Map;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text('${snapshot['name']} · SOP bản ${snapshot['version_no']}'),
        Text(
          '${snapshot['anchor_name']}: ${_day(widget.season['anchor_date'])} · ${widget.season['area_ha']} ha',
        ),
        Text(switch (widget.season['gps_missing']) {
          true => 'Thiếu GPS khi mở vụ',
          false => 'Đã ghi nhận vị trí khi mở vụ',
          _ => 'Chưa có thông tin vị trí vụ',
        }),
      ],
    );
  }

  Widget _task(
    HouseholdJson task,
    HouseholdJson data,
    List<HouseholdJson> records,
  ) {
    final record = records
        .where((r) => r['step_seq'] == task['seq'] && r['state'] == 'done')
        .firstOrNull;
    final parts = <String>[
      _labels[task['completion_status']] ?? '${task['completion_status']}',
      if (task['undated'] == true)
        'Chưa đặt ngày'
      else if (task['from_date'] != null)
        '${_day(task['from_date'])}${task['to_date'] == null ? ' trở đi' : ' – ${_day(task['to_date'])}'}',
      if (record != null) ...[
        'Làm xong ${_day(record['done_on'])}',
        if (record['gps_missing'] == true) 'Thiếu GPS',
        for (final input in _rows(record['inputs']))
          '${input['name']}: ${input['qty']} ${input['unit']}',
      ],
    ];
    if (widget.canWrite && task['can_complete'] == true) {
      return CheckboxListTile(
        title: Text('${task['name']}'),
        subtitle: Text(parts.join(' · ')),
        value: _pending?.seq == task['seq'],
        onChanged: _busy ? null : (_) => _select(task, data),
      );
    }
    return ListTile(
      title: Text('${task['name']}'),
      subtitle: Text(parts.join(' · ')),
    );
  }
}
