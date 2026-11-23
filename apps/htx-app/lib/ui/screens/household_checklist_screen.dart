import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/db/storage_errors.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:flutter/material.dart';

import '../../application/household_season_service.dart';
import '../widgets.dart' show OfflineBanner, rejectMessage;
import 'household_task_detail_screen.dart';

import 'package:agrigreen_mobile/domain/outbox_record.dart';

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
String _stateLabel(Object? state) => switch (state) {
  'done' => 'Đã xong',
  'partial' => 'Chưa xong, làm tiếp',
  'skipped' => 'Bỏ qua',
  'voided' => 'Đã xóa',
  _ => '$state',
};
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

  /// Lần gọi máy chủ gần nhất lỗi mạng (AH-114): hiện dải ngoại tuyến.
  bool _offline = false;
  String? _error;
  List<OutboxRecord> _queued = [];
  String get _season => widget.season['season_ref'] as String;
  @override
  void initState() {
    super.initState();
    _load = widget.service.checklist(widget.coop, widget.household, _season);
    _loadPending();
  }

  Future<void> _loadPending() async {
    final rows = await widget.service.pendingTasks(
      widget.coop,
      widget.household,
      _season,
    );
    if (mounted) setState(() => _queued = rows);
  }

  Future<void> _detail(HouseholdJson task, HouseholdJson data) async {
    final saved = await Navigator.push<bool>(
      context,
      MaterialPageRoute(
        builder: (_) => HouseholdTaskDetailScreen(
          service: widget.service,
          coop: widget.coop,
          household: widget.household,
          season: widget.season,
          task: task,
          event: '${data['current_event_id']}',
          offline: _offline || data['offline_cache'] == true,
        ),
      ),
    );
    if (saved == true && mounted) {
      await _loadPending();
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Đã lưu trên máy — chờ đồng bộ.')),
        );
      }
    }
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
      if (e is NetworkException && widget.service.canQueueOffline) {
        await _queueOffline(pending);
        return;
      }
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

  /// AH-114 AC1: mất mạng khi tích nhanh → lưu trên máy "Chờ đồng bộ".
  Future<void> _queueOffline(_PendingTask pending) async {
    try {
      await widget.service.queueTick(
        widget.coop,
        widget.household,
        widget.season,
        pending.seq,
        pending.event,
        await pending.gps,
      );
      if (!mounted) return;
      setState(() {
        _offline = true;
        _pending = null;
      });
      await _loadPending();
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Đã lưu trên máy — chờ đồng bộ.')),
        );
      }
    } catch (e) {
      if (!mounted) return;
      setState(
        () => _error = isStorageFull(e)
            ? msgStorageFull
            : 'Chưa lưu được trên máy. Thử lại; dữ liệu được giữ.',
      );
    }
  }

  /// AH-126 AC4, AC5: bỏ tích/xóa bắt buộc lý do ≥ 10 ký tự; không bỏ tích im lặng.
  Future<void> _void(HouseholdJson record) async {
    final untick = record['state'] == 'done';
    final key = _ids.newId();
    final reason = TextEditingController();
    String? error;
    var busy = false;
    final ok = await showDialog<bool>(
      context: context,
      builder: (c) => StatefulBuilder(
        builder: (c, set) => AlertDialog(
          title: Text(untick ? 'Bỏ tích việc đã xong' : 'Xóa bản ghi'),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                '${untick ? 'Nếu đây là bản ghi duy nhất, việc trở về chưa xong. ' : ''}Bản ghi bị ẩn, không xóa hẳn; lịch sử vẫn xem được.',
              ),
              TextField(
                key: const ValueKey('void-reason'),
                controller: reason,
                maxLength: 500,
                decoration: InputDecoration(
                  labelText: 'Lý do (ít nhất 10 ký tự)',
                  errorText: error,
                ),
              ),
            ],
          ),
          actions: [
            TextButton(
              onPressed: busy ? null : () => Navigator.pop(c, false),
              child: const Text('Hủy'),
            ),
            FilledButton(
              onPressed: busy
                  ? null
                  : () async {
                      if (reason.text.trim().length < 10) {
                        set(() => error = 'Nhập lý do (ít nhất 10 ký tự).');
                        return;
                      }
                      set(() {
                        busy = true;
                        error = null;
                      });
                      try {
                        final res = await widget.service.voidTask(
                          widget.coop,
                          widget.household,
                          _season,
                          '${record['log_ref']}',
                          '${record['id']}',
                          reason.text.trim(),
                          key,
                        );
                        if (!mounted) return;
                        setState(() {
                          _updated = Map<String, Object?>.from(
                            res['checklist'] as Map,
                          );
                          _load = widget.service.checklist(
                            widget.coop,
                            widget.household,
                            _season,
                          );
                        });
                        if (c.mounted) Navigator.pop(c, true);
                      } catch (e) {
                        set(() {
                          busy = false;
                          error = e is ApiException
                              ? (e.detail ?? 'Không lưu được. Tải lại rồi thử.')
                              : 'Cần mạng để xóa hoặc bỏ tích. Thử lại khi có mạng.';
                        });
                      }
                    },
              child: Text(untick ? 'Bỏ tích' : 'Xóa'),
            ),
          ],
        ),
      ),
    );
    reason.dispose();
    if (ok == true && mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(untick ? 'Đã bỏ tích.' : 'Đã xóa bản ghi.')),
      );
    }
  }

  /// AH-126: lịch sử các phiên bản (giá trị, người, thời điểm, lý do).
  Future<void> _history(HouseholdJson record) async {
    final items = widget.service.taskHistory(
      widget.coop,
      widget.household,
      _season,
      '${record['log_ref']}',
    );
    await showDialog<void>(
      context: context,
      builder: (c) => AlertDialog(
        title: const Text('Lịch sử bản ghi'),
        content: FutureBuilder<List<HouseholdJson>>(
          future: items,
          builder: (c, s) {
            if (s.hasError) return const Text('Cần mạng để xem lịch sử.');
            if (!s.hasData) return const LinearProgressIndicator();
            return SingleChildScrollView(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  for (final v in s.data!)
                    ListTile(
                      title: Text(
                        'Phiên bản ${v['version']} · ${_stateLabel(v['state'])} · làm ${_day(v['done_on'])}',
                      ),
                      subtitle: Text(
                        [
                          'Ghi lúc ${v['recorded_at']}',
                          if (v['correction_reason'] != null)
                            'Lý do: ${v['correction_reason']}',
                          if ('${v['note'] ?? ''}'.isNotEmpty) '${v['note']}',
                        ].join(' · '),
                      ),
                    ),
                ],
              ),
            );
          },
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(c),
            child: const Text('Đóng'),
          ),
        ],
      ),
    );
  }

  Future<void> _discard(OutboxRecord r) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (c) => AlertDialog(
        title: const Text('Xóa bản ghi chưa đồng bộ'),
        content: const Text(
          'Bản ghi đã bị máy chủ từ chối và chỉ còn trên máy. Xóa thì không khôi phục được.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(c, false),
            child: const Text('Giữ lại'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(c, true),
            child: const Text('Xóa'),
          ),
        ],
      ),
    );
    if (ok != true) return;
    await widget.service.discardTask(r.id);
    await _loadPending();
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
                if (_offline || data['offline_cache'] == true)
                  const OfflineBanner(),
                _header(),
                if (data['offline_cache'] == true)
                  const Text(
                    'Đang xem bản đã lưu trên máy. Máy chủ sẽ kiểm lại khi đồng bộ.',
                  ),
                for (final r in _queued)
                  ListTile(
                    title: Text(
                      r.status == OutboxStatus.rejected
                          ? 'Việc ${r.payload['step_seq']} · ${rejectMessage(r.errorCode)}'
                          : 'Việc ${r.payload['step_seq']} · ${r.status.label}',
                    ),
                    subtitle: Text(
                      '${r.payload['done_on']} · ${r.payload['state'] == 'partial' ? 'Làm tiếp' : 'Hoàn tất'}${r.errorSummary.isEmpty ? '' : ' · ${r.errorSummary}'}',
                    ),
                    trailing: r.status == OutboxStatus.rejected
                        ? TextButton(
                            onPressed: () => _discard(r),
                            child: const Text('Xóa'),
                          )
                        : null,
                  ),
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
    final logs = records.where((r) => r['step_seq'] == task['seq']).toList();
    final journal = [
      for (final r in logs)
        Row(
          children: [
            Expanded(
              child: Text(
                '${_stateLabel(r['state'])} · ${_day(r['done_on'])}${(r['version'] as num? ?? 1) > 1 ? ' · Đã sửa' : ''}',
              ),
            ),
            if (widget.canWrite)
              TextButton(
                onPressed: _busy ? null : () => _void(r),
                child: Text(r['state'] == 'done' ? 'Bỏ tích' : 'Xóa'),
              ),
            TextButton(
              onPressed: () => _history(r),
              child: const Text('Lịch sử'),
            ),
          ],
        ),
    ];
    if (widget.canWrite && task['can_complete'] == true) {
      return Column(
        children: [
          CheckboxListTile(
            title: Text('${task['name']}'),
            subtitle: Text(parts.join(' · ')),
            value: _pending?.seq == task['seq'],
            onChanged: _busy ? null : (_) => _select(task, data),
          ),
          TextButton(
            onPressed: _busy ? null : () => _detail(task, data),
            child: Text('Ghi chi tiết ${task['name']}'),
          ),
          ...journal,
        ],
      );
    }
    return Column(
      children: [
        ListTile(
          title: Text('${task['name']}'),
          subtitle: Text(parts.join(' · ')),
        ),
        ...journal,
      ],
    );
  }
}
