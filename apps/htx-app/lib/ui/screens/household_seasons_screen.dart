import 'package:agrigreen_mobile/core/ids.dart';
import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/household_season_service.dart';
import '../../application/manager_service.dart';
import '../../domain/roles.dart';
import '../../domain/season_plan.dart';
import '../season_plan_view.dart';
import '../providers.dart';
import 'households_screen.dart';
import 'open_season_screen.dart' show parseDisplayDay;
import 'household_checklist_screen.dart';

class _HouseholdSeasonsData {
  const _HouseholdSeasonsData(this.coop, this.household, this.seasons);
  final String coop;
  final HouseholdItem household;
  final List<HouseholdJson> seasons;
}

class HouseholdSeasonsScreen extends ConsumerStatefulWidget {
  const HouseholdSeasonsScreen({super.key, required this.household});
  final HouseholdItem household;
  @override
  ConsumerState<HouseholdSeasonsScreen> createState() =>
      _HouseholdSeasonsScreenState();
}

class _HouseholdSeasonsScreenState
    extends ConsumerState<HouseholdSeasonsScreen> {
  late Future<_HouseholdSeasonsData> _data;
  @override
  void initState() {
    super.initState();
    _data = _load();
  }

  Future<_HouseholdSeasonsData> _load() async {
    final services = ref.services;
    final coop = await services.profile.cooperativeRef();
    if (coop == null || coop.isEmpty) {
      throw StateError('Chưa có HTX trong phiên.');
    }
    final households = await services.manager.households(cooperativeRef: coop);
    final h = households
        .where((h) => h.ref == widget.household.ref)
        .firstOrNull;
    if (h == null) throw StateError('Không còn tìm thấy hộ trong HTX.');
    return _HouseholdSeasonsData(
      coop,
      h,
      await services.crop.householdSeasons.seasons(coop, h.ref),
    );
  }

  void _reload() => setState(() {
    _data = _load();
  });
  void _show(String coop, HouseholdJson season) {
    Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) => HouseholdChecklistScreen(
          service: ref.services.crop.householdSeasons,
          coop: coop,
          household: widget.household.ref,
          season: season,
          canWrite: ref.read(appControllerProvider).mode == HtxMode.member,
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final mode = ref.watch(appControllerProvider.select((s) => s.mode));
    return Scaffold(
      appBar: AppBar(
        title: Text('Vụ của ${widget.household.label}'),
        actions: [
          IconButton(
            onPressed: _reload,
            icon: const Icon(Icons.refresh),
            tooltip: 'Tải lại',
          ),
        ],
      ),
      body: Column(
        children: [
          TextButton(
            onPressed: () => Navigator.of(context).push(
              MaterialPageRoute<void>(
                builder: (_) =>
                    HouseholdPlotsScreen(household: widget.household),
              ),
            ),
            child: const Text('Xem thửa và vụ theo thửa đang có'),
          ),
          Expanded(
            child: FutureBuilder<_HouseholdSeasonsData>(
              future: _data,
              builder: (context, state) {
                if (state.connectionState != ConnectionState.done) {
                  return const Center(child: CircularProgressIndicator());
                }
                if (state.hasError) {
                  return Center(
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        const Text('Cần mạng để tải vụ và diện tích hiện tại.'),
                        TextButton(
                          onPressed: _reload,
                          child: const Text('Thử lại'),
                        ),
                      ],
                    ),
                  );
                }
                final d = state.requireData;
                return ListView(
                  padding: const EdgeInsets.all(16),
                  children: [
                    Text(
                      'Diện tích còn lại: ${d.household.remainingAreaHa ?? '—'} ha',
                    ),
                    if (mode == HtxMode.member &&
                        d.household.areaFresh &&
                        d.household.canOpenSeason)
                      FilledButton.icon(
                        icon: const Icon(Icons.add),
                        label: const Text('Mở vụ mới'),
                        onPressed: () async {
                          final saved = await Navigator.of(context).push<bool>(
                            MaterialPageRoute<bool>(
                              builder: (_) => HouseholdSeasonOpenScreen(
                                coop: d.coop,
                                household: d.household,
                              ),
                            ),
                          );
                          if (saved == true && mounted) _reload();
                        },
                      ),
                    if (mode == HtxMode.member && !d.household.canOpenSeason)
                      Text(d.household.blockedReason),
                    if (d.seasons.isEmpty) const Text('Chưa có vụ theo hộ.'),
                    for (final s in d.seasons)
                      Card(
                        child: ListTile(
                          title: Text(
                            '${s['season_code']} ${s['season_year']} · ${s['crop_code']}',
                          ),
                          subtitle: Text(
                            '${s['area_ha']} ha · ${s['status'] == 'open' ? 'Đang canh tác' : s['status']}',
                          ),
                          onTap: () => _show(d.coop, s),
                        ),
                      ),
                  ],
                );
              },
            ),
          ),
        ],
      ),
    );
  }
}

class HouseholdSeasonOpenScreen extends ConsumerStatefulWidget {
  const HouseholdSeasonOpenScreen({
    super.key,
    required this.coop,
    required this.household,
    this.calendarPrefill,
  });
  final String coop;
  final HouseholdItem household;
  final SeasonCalendarPrefill? calendarPrefill;
  @override
  ConsumerState<HouseholdSeasonOpenScreen> createState() =>
      _HouseholdSeasonOpenScreenState();
}

class _HouseholdSeasonOpenScreenState
    extends ConsumerState<HouseholdSeasonOpenScreen> {
  final _area = TextEditingController();
  final _year = TextEditingController(text: '${DateTime.now().year}');
  final _anchor = TextEditingController();
  final _ids = UuidV7Generator();
  late String _key;
  late String _previewKey;
  SeasonPlan? _plan;
  late Future<List<List<HouseholdJson>>> _catalogs;
  Future<List<HouseholdSopChoice>>? _sops;
  List<HouseholdSopChoice> _options = [];
  String? _crop, _variety, _season, _sop;
  HouseholdJson? _gps;
  String? _error, _remaining;
  int _step = 0;
  bool _saving = false;
  HouseholdSeasonService get _service => ref.services.crop.householdSeasons;
  @override
  void initState() {
    super.initState();
    _key = _ids.newId();
    _previewKey = _ids.newId();
    _remaining = widget.household.remainingAreaHa;
    final prefill = widget.calendarPrefill;
    if (prefill != null) {
      _crop = prefill.cropCode;
      _season = prefill.seasonCode;
      _year.text = '${prefill.year}';
      _anchor.text = prefill.displayAnchor;
    }
    _catalogs =
        Future.wait(['crop', 'rice_variety', 'season'].map(_service.catalog))
            .then((rows) {
              if (!rows[0].any(
                (c) =>
                    c['code'] == _crop &&
                    ((c['attrs'] as Map?)?['parent_code'] == null ||
                        (c['attrs'] as Map?)?['parent_code'] == ''),
              )) {
                _crop = null;
              }
              if (!rows[2].any((s) => s['code'] == _season)) {
                _season = null;
              }
              return rows;
            });
  }

  @override
  void dispose() {
    _area.dispose();
    _year.dispose();
    _anchor.dispose();
    super.dispose();
  }

  void _edit() {
    _key = _ids.newId();
    _previewKey = _ids.newId();
    _plan = null;
    _error = null;
  }

  void _loadSops() {
    _sop = null;
    _options = [];
    _sops = _crop == null || _variety == null
        ? null
        : _service.sops(widget.coop, _crop!, _variety!);
  }

  Future<void> _next() async {
    String? error;
    if (_step == 0 &&
        (_crop == null ||
            _season == null ||
            (int.tryParse(_year.text) ?? 0) < 2000 ||
            (int.tryParse(_year.text) ?? 0) > 2100)) {
      error = 'Chọn loại cây, mùa vụ và năm 2000–2100.';
    }
    if (_step == 1) {
      error = _variety == null ? 'Chọn giống.' : householdAreaError(_area.text);
    }
    final chosen = _sop ?? (_options.length == 1 ? _options.first.id : null);
    if (_step == 2 &&
        (chosen == null || parseDisplayDay(_anchor.text) == null)) {
      error = 'Chọn SOP đang áp dụng và mốc gốc hợp lệ dd/mm/yyyy.';
    }
    if (error != null) {
      setState(() => _error = error);
      return;
    }
    if (_step == 2 && _plan == null) {
      setState(() => _saving = true);
      try {
        final date = parseDisplayDay(_anchor.text)!;
        final plan = await _service.previewPlan(
          widget.coop,
          widget.household.ref,
          {
            'crop_code': _crop,
            'variety_code': _variety,
            'sop_version_id': chosen,
            'area_ha': _area.text,
            'anchor_date': date.toIso8601String().substring(0, 10),
          },
          _previewKey,
        );
        if (!mounted) return;
        setState(() => _plan = plan);
      } catch (e) {
        if (mounted) {
          setState(
            () => _error = e is ApiException
                ? (e.detail ??
                      'Không tính được kế hoạch. Kiểm tra dữ liệu rồi thử lại.')
                : 'Cần mạng để tính kế hoạch. Dữ liệu nhập được giữ lại.',
          );
        }
        return;
      } finally {
        if (mounted) setState(() => _saving = false);
      }
    }
    if (_step == 3 && _gps == null) {
      setState(() => _saving = true);
      final gps = await _service.gps();
      if (!mounted) return;
      setState(() {
        _gps = gps;
        _saving = false;
      });
    }
    if (_step < 4) {
      setState(() {
        _step++;
        _error = null;
      });
      return;
    }
    await _save(chosen!);
  }

  Future<void> _save(String chosen) async {
    if (ref.read(appControllerProvider).mode != HtxMode.member) return;
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      final date = parseDisplayDay(_anchor.text)!;
      await _service.open(widget.coop, widget.household.ref, {
        'crop_code': _crop,
        'season_code': _season,
        'season_year': int.parse(_year.text),
        'variety_code': _variety,
        'area_ha': _area.text,
        'anchor_date':
            '${date.year.toString().padLeft(4, '0')}-${date.month.toString().padLeft(2, '0')}-${date.day.toString().padLeft(2, '0')}',
        'sop_version_id': chosen,
        'gps': _gps,
      }, _key);
      if (mounted) Navigator.of(context).pop(true);
    } on ApiException catch (e) {
      if (mounted) {
        setState(() {
          _remaining = e.problem['remaining_area_ha'] as String? ?? _remaining;
          final fields = (e.problem['errors'] as List? ?? const [])
              .whereType<Map<Object?, Object?>>()
              .map((e) => '${e['reason']}')
              .join('\n');
          _error =
              '${e.detail ?? 'Không thể lưu vụ.'}${fields.isEmpty ? '' : '\n$fields'}${e.code == 'cropcycle.household_area_exceeded' ? '\nCòn lại: $_remaining ha' : ''}';
        });
      }
    } catch (_) {
      if (mounted) {
        setState(
          () => _error =
              'Không thể lưu. Mở vụ cần mạng; form được giữ để thử lại.',
        );
      }
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  Widget _select(
    String label,
    String? value,
    List<HouseholdJson> items,
    void Function(String?) change,
  ) => DropdownButtonFormField<String>(
    key: ValueKey('$label-$value-${items.length}'),
    initialValue: value,
    isExpanded: true,
    decoration: InputDecoration(labelText: label),
    items: items
        .map(
          (e) => DropdownMenuItem(
            value: '${e['code']}',
            child: Text('${e['name']}'),
          ),
        )
        .toList(),
    onChanged: _saving ? null : change,
  );
  @override
  Widget build(BuildContext context) {
    if (ref.watch(appControllerProvider.select((s) => s.mode)) !=
        HtxMode.member) {
      return const Scaffold(
        body: Center(child: Text('Vai của bạn chỉ được xem vụ.')),
      );
    }
    return Scaffold(
      appBar: AppBar(title: const Text('Khai báo vụ mới')),
      body: FutureBuilder<List<List<HouseholdJson>>>(
        future: _catalogs,
        builder: (context, state) {
          if (state.hasError) {
            return const Center(
              child: Text('Cần mạng để tải danh mục. Quay lại và thử lại.'),
            );
          }
          if (!state.hasData) {
            return const Center(child: CircularProgressIndicator());
          }
          final catalogs = state.requireData;
          return Column(
            children: [
              Padding(
                padding: const EdgeInsets.all(12),
                child: Text(
                  '${widget.household.label} · Còn lại: ${_remaining ?? '—'} ha',
                ),
              ),
              if (_error != null)
                Padding(
                  padding: const EdgeInsets.all(12),
                  child: Text(
                    _error!,
                    style: TextStyle(
                      color: Theme.of(context).colorScheme.error,
                    ),
                  ),
                ),
              Expanded(
                child: Stepper(
                  currentStep: _step,
                  onStepContinue: _saving ? null : _next,
                  onStepCancel: _saving || _step == 0
                      ? null
                      : () => setState(() => _step--),
                  controlsBuilder: (context, controls) {
                    if (controls.stepIndex != _step) {
                      return const SizedBox.shrink();
                    }
                    return Row(
                      children: [
                        FilledButton(
                          onPressed: _saving ? null : controls.onStepContinue,
                          child: Text(
                            _saving
                                ? 'Đang xử lý…'
                                : _step == 4
                                ? 'Lưu vụ'
                                : 'Tiếp tục',
                          ),
                        ),
                        if (_step > 0)
                          TextButton(
                            onPressed: _saving ? null : controls.onStepCancel,
                            child: const Text('Quay lại'),
                          ),
                      ],
                    );
                  },
                  steps: [
                    Step(
                      title: const Text('Loại cây và mùa vụ'),
                      isActive: _step >= 0,
                      content: Column(
                        children: [
                          _select(
                            'Loại cây',
                            _crop,
                            catalogs[0]
                                .where(
                                  (c) =>
                                      ((c['attrs'] as Map?)?['parent_code'] ??
                                          '') ==
                                      '',
                                )
                                .toList(),
                            (v) => setState(() {
                              _crop = v;
                              _variety = null;
                              _loadSops();
                              _edit();
                            }),
                          ),
                          _select(
                            'Mùa vụ',
                            _season,
                            catalogs[2],
                            (v) => setState(() {
                              _season = v;
                              _edit();
                            }),
                          ),
                          TextField(
                            controller: _year,
                            enabled: !_saving,
                            keyboardType: TextInputType.number,
                            decoration: const InputDecoration(labelText: 'Năm'),
                            onChanged: (_) => setState(_edit),
                          ),
                        ],
                      ),
                    ),
                    Step(
                      title: const Text('Giống và diện tích'),
                      isActive: _step >= 1,
                      content: Column(
                        children: [
                          _select(
                            'Giống',
                            _variety,
                            catalogs[1]
                                .where(
                                  (v) =>
                                      (v['attrs'] as Map?)?['crop_code'] ==
                                      _crop,
                                )
                                .toList(),
                            (v) => setState(() {
                              _variety = v;
                              _loadSops();
                              _edit();
                            }),
                          ),
                          TextField(
                            controller: _area,
                            enabled: !_saving,
                            keyboardType: const TextInputType.numberWithOptions(
                              decimal: true,
                            ),
                            decoration: const InputDecoration(
                              labelText: 'Diện tích vụ (ha)',
                              hintText: 'Ví dụ: 0.25',
                            ),
                            onChanged: (_) => setState(_edit),
                          ),
                        ],
                      ),
                    ),
                    Step(
                      title: const Text('SOP và mốc gốc'),
                      isActive: _step >= 2,
                      content: Column(
                        children: [
                          if (_sops != null)
                            FutureBuilder<List<HouseholdSopChoice>>(
                              future: _sops,
                              builder: (context, data) {
                                if (data.hasError) {
                                  return const Text(
                                    'Không tải được SOP; quay lại chọn giống để thử lại.',
                                  );
                                }
                                if (data.connectionState !=
                                        ConnectionState.done ||
                                    !data.hasData) {
                                  return const CircularProgressIndicator();
                                }
                                _options = data.requireData;
                                if (_options.isEmpty) {
                                  return const Text(
                                    'Chưa có SOP đang áp dụng. Vui lòng liên hệ Admin.',
                                  );
                                }
                                return DropdownButtonFormField<String>(
                                  key: ValueKey(_sops),
                                  initialValue:
                                      _sop ??
                                      (_options.length == 1
                                          ? _options.first.id
                                          : null),
                                  isExpanded: true,
                                  decoration: const InputDecoration(
                                    labelText: 'SOP đang áp dụng',
                                  ),
                                  items: _options
                                      .map(
                                        (s) => DropdownMenuItem(
                                          value: s.id,
                                          child: Text(s.name),
                                        ),
                                      )
                                      .toList(),
                                  onChanged: _saving
                                      ? null
                                      : (v) => setState(() {
                                          _sop = v;
                                          _edit();
                                        }),
                                );
                              },
                            ),
                          TextField(
                            controller: _anchor,
                            enabled: !_saving,
                            decoration: const InputDecoration(
                              labelText: 'Mốc gốc (dd/mm/yyyy)',
                              helperText: 'Bạn tự chọn mốc gốc, không giới hạn −15/+30 ngày.',
                            ),
                            onChanged: (_) => setState(_edit),
                          ),
                        ],
                      ),
                    ),
                    Step(
                      title: const Text('Kế hoạch'),
                      isActive: _step >= 3,
                      content: _plan == null
                          ? const Text(
                              'Chọn giống, SOP và mốc gốc để tính kế hoạch.',
                            )
                          : SeasonPlanView(plan: _plan!),
                    ),
                    Step(
                      title: const Text('Xác nhận'),
                      isActive: _step >= 4,
                      content: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text('$_crop · $_season ${_year.text} · $_variety'),
                          Text('${_area.text} ha · Mốc gốc ${_anchor.text}'),
                          Text(
                            _gps?['source'] == 'device'
                                ? 'Đã ghi nhận vị trí.'
                                : 'Thiếu GPS — vẫn lưu được.',
                          ),
                          const Text(
                            'Mở vụ cần mạng. SOP được máy chủ chụp lại tại lúc lưu.',
                          ),
                        ],
                      ),
                    ),
                  ],
                ),
              ),
            ],
          );
        },
      ),
    );
  }
}
