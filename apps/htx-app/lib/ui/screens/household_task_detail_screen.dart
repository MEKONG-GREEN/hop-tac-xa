import 'package:agrigreen_mobile/data/db/storage_errors.dart';
import 'package:flutter/material.dart';

import '../../application/household_season_service.dart';
import '../../domain/cropcycle.dart' show parseScaled, formatScaled;
import '../widgets.dart' show OfflineBanner, PhotoCapture;

String? taskActualTotal(String dose, String area) {
  if (!RegExp(r'^(0|[1-9][0-9]{0,8})(\.[0-9]{1,4})?$').hasMatch(dose)) {
    return null;
  }
  final d = parseScaled(dose, 4), a = parseScaled(area, 2);
  if (d == null || a == null) return null;
  return formatScaled(d * a, 6);
}

class HouseholdTaskDetailScreen extends StatefulWidget {
  const HouseholdTaskDetailScreen({
    super.key,
    required this.service,
    required this.coop,
    required this.household,
    required this.season,
    required this.task,
    required this.event,
    this.offline = false,
  });
  final HouseholdSeasonService service;
  final String coop, household, event;

  /// Đang ngoại tuyến (AH-114): hiện dải báo; bản ghi vẫn lưu trên máy như thường.
  final bool offline;
  final HouseholdJson season, task;
  @override
  State<HouseholdTaskDetailScreen> createState() => _HouseholdTaskDetailState();
}

class _Material {
  _Material(HouseholdJson m)
    : code = '${m['material_code'] ?? ''}',
      name = TextEditingController(text: '${m['name'] ?? ''}'),
      unit = TextEditingController(text: '${m['unit'] ?? ''}'),
      dose = TextEditingController(text: '${m['dose_per_ha'] ?? '0'}');
  String code;
  final TextEditingController name, unit, dose;
  HouseholdJson get body => {
    'material_code': code,
    'name': name.text.trim(),
    'unit': unit.text.trim(),
    'dose_per_ha': dose.text.trim(),
  };
  void dispose() {
    name.dispose();
    unit.dispose();
    dose.dispose();
  }
}

class _HouseholdTaskDetailState extends State<HouseholdTaskDetailScreen> {
  final _form = GlobalKey<FormState>();
  final _day = TextEditingController();
  final _note = TextEditingController();
  final _reason = TextEditingController();
  final _materials = <_Material>[];
  List<String> _photos = [];
  bool _zero = false, _busy = false;
  String _state = 'done';
  String? _error;
  late final Future<List<HouseholdJson>> _catalog;
  late final bool _hasRecommendation;
  @override
  void initState() {
    super.initState();
    final now = DateTime.now();
    _day.text =
        '${now.year.toString().padLeft(4, '0')}-${now.month.toString().padLeft(2, '0')}-${now.day.toString().padLeft(2, '0')}';
    final step = HouseholdSeasonService.effectiveStep(
      widget.season['sop_snapshot'],
      widget.task['seq'],
    );
    final recommended = (step?['materials'] as List? ?? [])
        .cast<Map<Object?, Object?>>();
    _hasRecommendation = recommended.isNotEmpty;
    for (final m in recommended) {
      _materials.add(_Material(m.cast<String, Object?>()));
    }
    _catalog = widget.service.catalog('material');
  }

  @override
  void dispose() {
    _day.dispose();
    _note.dispose();
    _reason.dispose();
    for (final m in _materials) {
      m.dispose();
    }
    super.dispose();
  }

  bool get _needsZero =>
      (_hasRecommendation && _materials.isEmpty) ||
      _materials.any((m) => RegExp(r'^0(?:\.0+)?$').hasMatch(m.dose.text));
  bool get _needsReason {
    final day = DateTime.tryParse(_day.text);
    final now = DateTime.now();
    return widget.task['undated'] != true &&
        day != null &&
        day.isBefore(
          DateTime(
            now.year,
            now.month,
            now.day,
          ).subtract(const Duration(days: 3)),
        );
  }

  Future<void> _save() async {
    if (_busy || !_form.currentState!.validate()) return;
    if (_needsZero && !_zero) {
      setState(() => _error = 'Xác nhận liều bằng 0 trước khi lưu.');
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final point = await widget.service.gps().timeout(
        const Duration(seconds: 2),
        onTimeout: () => {'source': 'no_device'},
      );
      await widget.service.recordTask(
        widget.coop,
        widget.household,
        '${widget.season['season_ref']}',
        {
          'expected_season_event_id': widget.event,
          'step_seq': widget.task['seq'],
          'state': _state,
          'done_on': _day.text,
          'materials': _materials.map((m) => m.body).toList(),
          'file_refs': _photos,
          'note': _note.text,
          'backfill_reason': _reason.text,
          'zero_confirmed': _zero,
          'gps': point,
        },
      );
      if (mounted) Navigator.pop(context, true);
    } catch (e) {
      if (mounted) {
        setState(
          () => _error = isStorageFull(e)
              ? msgStorageFull
              : 'Chưa lưu được trên máy. Dữ liệu form được giữ để thử lại.',
        );
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: Text('Ghi chi tiết ${widget.task['name']}')),
    body: SingleChildScrollView(
      padding: const EdgeInsets.all(16),
      child: Form(
        key: _form,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (widget.offline) const OfflineBanner(),
            Text(
              'Diện tích vụ: ${widget.season['area_ha']} ha. Liều thực tế lưu riêng với SOP.',
            ),
            TextFormField(
              controller: _day,
              enabled: !_busy,
              decoration: const InputDecoration(
                labelText: 'Ngày làm xong (yyyy-mm-dd)',
              ),
              onChanged: (_) => setState(() {}),
              validator: (v) {
                final d = DateTime.tryParse('$v');
                if (d == null ||
                    d.toIso8601String().substring(0, 10) != v ||
                    d.isAfter(DateTime.now())) {
                  return 'Ngày làm xong không hợp lệ hoặc sau hôm nay.';
                }
                return null;
              },
            ),
            DropdownButtonFormField<String>(
              initialValue: _state,
              decoration: const InputDecoration(labelText: 'Kết quả'),
              items: const [
                DropdownMenuItem(
                  value: 'done',
                  child: Text('Đã xong hoàn toàn'),
                ),
                DropdownMenuItem(
                  value: 'partial',
                  child: Text('Chưa xong, sẽ làm tiếp lần sau'),
                ),
              ],
              onChanged: _busy ? null : (v) => setState(() => _state = v!),
            ),
            FutureBuilder<List<HouseholdJson>>(
              future: _catalog,
              builder: (context, data) {
                final catalog = data.data ?? [];
                return Column(
                  children: [
                    if (data.hasError)
                      const Text(
                        'Chưa tải được danh mục; kết nối mạng một lần để chuẩn bị ghi ngoại tuyến.',
                      ),
                    for (final m in _materials)
                      Padding(
                        key: ObjectKey(m),
                        padding: const EdgeInsets.symmetric(vertical: 10),
                        child: Column(
                          children: [
                            DropdownButtonFormField<String>(
                              key: ValueKey(
                                '${identityHashCode(m)}:${m.code}:${catalog.length}',
                              ),
                              initialValue: m.code,
                              isExpanded: true,
                              decoration: const InputDecoration(
                                labelText: 'Vật tư',
                              ),
                              items: [
                                const DropdownMenuItem(
                                  value: '',
                                  child: Text(
                                    'Chưa có trong danh mục — nhập tên',
                                  ),
                                ),
                                if (m.code.isNotEmpty &&
                                    !catalog.any((c) => c['code'] == m.code))
                                  DropdownMenuItem(
                                    value: m.code,
                                    child: Text('${m.name.text} (SOP)'),
                                  ),
                                for (final c in catalog.where(
                                  (c) =>
                                      c['active'] != false ||
                                      c['code'] == m.code,
                                ))
                                  DropdownMenuItem(
                                    value: '${c['code']}',
                                    child: Text('${c['code']} · ${c['name']}'),
                                  ),
                              ],
                              onChanged: _busy
                                  ? null
                                  : (v) => setState(() {
                                      m.code = v!;
                                      final c = catalog
                                          .where((c) => c['code'] == v)
                                          .firstOrNull;
                                      m.name.text = '${c?['name'] ?? ''}';
                                      m.unit.text =
                                          '${(c?['attrs'] as Map?)?['unit'] ?? ''}';
                                    }),
                            ),
                            if (m.code.isEmpty) ...[
                              TextFormField(
                                controller: m.name,
                                enabled: !_busy,
                                decoration: const InputDecoration(
                                  labelText: 'Tên vật tư',
                                ),
                                validator: (v) => v == null || v.trim().isEmpty
                                    ? 'Nhập tên vật tư.'
                                    : null,
                              ),
                              TextFormField(
                                controller: m.unit,
                                enabled: !_busy,
                                decoration: const InputDecoration(
                                  labelText: 'Đơn vị',
                                ),
                                validator: (v) => v == null || v.trim().isEmpty
                                    ? 'Nhập đơn vị.'
                                    : null,
                              ),
                            ],
                            TextFormField(
                              controller: m.dose,
                              enabled: !_busy,
                              decoration: InputDecoration(
                                labelText: 'Liều thực tế/ha (${m.unit.text})',
                              ),
                              keyboardType:
                                  const TextInputType.numberWithOptions(
                                    decimal: true,
                                  ),
                              onChanged: (_) => setState(() {}),
                              validator: (v) =>
                                  taskActualTotal(
                                        '$v',
                                        '${widget.season['area_ha']}',
                                      ) ==
                                      null
                                  ? 'Liều không âm, tối đa 4 chữ số thập phân.'
                                  : null,
                            ),
                            Text(
                              'Tổng thực tế: ${taskActualTotal(m.dose.text, '${widget.season['area_ha']}') ?? '—'} ${m.unit.text}',
                            ),
                            TextButton(
                              onPressed: _busy
                                  ? null
                                  : () => setState(() {
                                      _materials.remove(m);
                                      m.dispose();
                                    }),
                              child: const Text('Bỏ vật tư'),
                            ),
                          ],
                        ),
                      ),
                    TextButton(
                      onPressed:
                          _busy || !data.hasData || _materials.length >= 20
                          ? null
                          : () => setState(() => _materials.add(_Material({}))),
                      child: const Text('Thêm vật tư'),
                    ),
                  ],
                );
              },
            ),
            if (_needsZero)
              CheckboxListTile(
                value: _zero,
                onChanged: _busy ? null : (v) => setState(() => _zero = v!),
                title: const Text(
                  'Tôi xác nhận liều bằng 0 hoặc không sử dụng vật tư khuyến cáo',
                ),
              ),
            TextFormField(
              controller: _note,
              enabled: !_busy,
              maxLength: 2000,
              decoration: const InputDecoration(labelText: 'Ghi chú'),
            ),
            TextFormField(
              controller: _reason,
              enabled: !_busy,
              maxLength: 500,
              decoration: InputDecoration(
                labelText: _needsReason
                    ? 'Lý do ghi bù (bắt buộc)'
                    : 'Lý do ghi bù',
              ),
              validator: (v) => _needsReason && (v == null || v.trim().isEmpty)
                  ? 'Nhập lý do ghi bù quá 3 ngày.'
                  : null,
            ),
            if (!_busy)
              PhotoCapture(
                householdRef: widget.household,
                photoIds: _photos,
                max: 5,
                maxPlainBytes: 1024 * 1024,
                parentEntity: 'household_task_record',
                beforeCapture: () =>
                    widget.service.preparePhotoKey(widget.household),
                onChanged: (v) => setState(() => _photos = v),
              ),
            for (var i = 0; i < _photos.length; i++)
              TextButton(
                onPressed: _busy
                    ? null
                    : () => setState(() => _photos.removeAt(i)),
                child: Text('Bỏ ảnh ${i + 1}'),
              ),
            const Text(
              'Lưu trên máy trước, ảnh mã hóa được tải trước nhật ký khi có mạng. Máy chủ kiểm quyền và trạng thái vụ khi đồng bộ.',
            ),
            if (_error != null) Text(_error!),
            FilledButton(
              onPressed: _busy ? null : _save,
              child: Text(_busy ? 'Đang lưu…' : 'Lưu nhật ký chờ đồng bộ'),
            ),
          ],
        ),
      ),
    ),
  );
}
