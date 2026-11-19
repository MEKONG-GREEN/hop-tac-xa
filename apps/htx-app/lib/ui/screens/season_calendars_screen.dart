import 'package:agrigreen_mobile/data/sync/sync_api.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/household_season_service.dart';
import '../../application/manager_service.dart';
import '../../domain/roles.dart';
import '../../domain/role_matrix.dart';
import '../providers.dart';
import 'household_seasons_screen.dart';

class _CalendarData {
  const _CalendarData(this.coop, this.calendars, this.crops, this.seasons);
  final String coop;
  final List<HouseholdJson> calendars;
  final Map<String, String> crops, seasons;
}

/// Guidance is read online. Opening a season copies editable suggestions; no
/// calendar reference/date-window constraint is added to the opened season.
class SeasonCalendarsScreen extends ConsumerStatefulWidget {
  const SeasonCalendarsScreen({super.key, this.calendarRef});
  final String? calendarRef;
  @override
  ConsumerState<SeasonCalendarsScreen> createState() =>
      _SeasonCalendarsScreenState();
}

class _SeasonCalendarsScreenState extends ConsumerState<SeasonCalendarsScreen> {
  late Future<_CalendarData> _future;
  bool _opening = false;
  String? _error;
  @override
  void initState() {
    super.initState();
    _future = ref.read(appControllerProvider).mode == HtxMode.member
        ? _load()
        : Future.value(const _CalendarData('', [], {}, {}));
  }

  @override
  void didUpdateWidget(covariant SeasonCalendarsScreen oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.calendarRef != widget.calendarRef &&
        ref
            .read(appControllerProvider)
            .mode
            .canView(HtxScreen.seasonCalendars)) {
      _future = _load();
    }
  }

  Future<_CalendarData> _load() async {
    final services = ref.services;
    final coop = await services.profile.cooperativeRef();
    if (coop == null || coop.isEmpty) {
      throw StateError('Chưa có HTX trong phiên.');
    }
    final service = services.crop.householdSeasons;
    final rows = widget.calendarRef == null
        ? await service.calendars(coop)
        : [await service.calendar(widget.calendarRef!)];
    final calendars = rows
        .where(
          (c) => c['cooperative_ref'] == coop && c['status'] == 'published',
        )
        .toList();
    if (widget.calendarRef != null && calendars.isEmpty) {
      throw ApiException(
        404,
        'cropcycle.calendar_not_found',
        'Lịch đã hủy hoặc không thuộc phạm vi hiện tại.',
      );
    }
    for (final c in calendars) {
      SeasonCalendarPrefill.fromJson(c);
    }
    var crops = <String, String>{};
    var seasons = <String, String>{};
    try {
      final labels = await Future.wait(['crop', 'season'].map(service.catalog));
      crops = {for (final c in labels[0]) '${c['code']}': '${c['name']}'};
      seasons = {for (final c in labels[1]) '${c['code']}': '${c['name']}'};
    } on Exception {
      // Calendar remains readable by its business codes.
      crops = {};
      seasons = {};
    }
    return _CalendarData(coop, calendars, crops, seasons);
  }

  void _reload() {
    setState(() {
      _error = null;
      _future = _load();
    });
  }

  String _day(Object? day) {
    final parts = '$day'.split('-');
    return parts.length == 3 ? '${parts[2]}/${parts[1]}/${parts[0]}' : '$day';
  }

  Future<void> _open(_CalendarData data, HouseholdJson calendar) async {
    if (_opening ||
        ref.read(appControllerProvider).mode != HtxMode.member ||
        !ref
            .read(appControllerProvider)
            .mode
            .can(HtxFn.season, HtxAction.create)) {
      return;
    }
    setState(() {
      _opening = true;
      _error = null;
    });
    try {
      final households = await ref.services.manager.households(
        cooperativeRef: data.coop,
      );
      if (!mounted || ref.read(appControllerProvider).mode != HtxMode.member) {
        return;
      }
      final available = households
          .where((h) => h.canOpenSeason && h.areaFresh)
          .toList();
      final selected = await showModalBottomSheet<HouseholdItem>(
        context: context,
        builder: (context) => SafeArea(
          child: SizedBox(
            height: MediaQuery.sizeOf(context).height * .6,
            child: ListView(
              padding: const EdgeInsets.all(16),
              children: [
                const Text(
                  'Chọn nông hộ để mở vụ',
                  style: TextStyle(fontSize: 20),
                ),
                if (available.isEmpty)
                  const Text('Chưa có hộ đủ điều kiện mở vụ.'),
                for (final h in available)
                  ListTile(
                    title: Text(h.label),
                    subtitle: Text('Còn lại: ${h.remainingAreaHa ?? '—'} ha'),
                    onTap: () => Navigator.of(context).pop(h),
                  ),
              ],
            ),
          ),
        ),
      );
      if (selected == null ||
          !mounted ||
          ref.read(appControllerProvider).mode != HtxMode.member) {
        return;
      }
      final saved = await Navigator.of(context).push<bool>(
        MaterialPageRoute(
          builder: (_) => HouseholdSeasonOpenScreen(
            coop: data.coop,
            household: selected,
            calendarPrefill: SeasonCalendarPrefill.fromJson(calendar),
          ),
        ),
      );
      if (saved == true && mounted) {
        _reload();
      }
    } on ApiException catch (e) {
      if (mounted) {
        setState(() => _error = e.detail ?? 'Không thể mở vụ theo lịch.');
      }
    } catch (_) {
      if (mounted) {
        setState(
          () => _error = 'Cần mạng để tải nông hộ và diện tích hiện tại.',
        );
      }
    } finally {
      if (mounted) setState(() => _opening = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final mode = ref.watch(appControllerProvider.select((s) => s.mode));
    if (!mode.canView(HtxScreen.seasonCalendars)) {
      return Scaffold(
        appBar: AppBar(title: const Text('Lịch thời vụ')),
        body: const Center(
          child: Text('Lịch thời vụ trên mobile dành cho Thành viên HTX.'),
        ),
      );
    }
    return Scaffold(
      appBar: AppBar(
        title: const Text('Lịch thời vụ'),
        actions: [
          IconButton(
            onPressed: _reload,
            icon: const Icon(Icons.refresh),
            tooltip: 'Tải lại',
          ),
        ],
      ),
      body: FutureBuilder<_CalendarData>(
        future: _future,
        builder: (context, state) {
          if (state.connectionState != ConnectionState.done) {
            return const Center(child: CircularProgressIndicator());
          }
          if (state.hasError) {
            final e = state.error;
            return Center(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(
                    e is ApiException
                        ? (e.detail ?? 'Không đọc được lịch.')
                        : 'Cần mạng để tải lịch thời vụ.',
                  ),
                  TextButton(onPressed: _reload, child: const Text('Thử lại')),
                ],
              ),
            );
          }
          final data = state.requireData;
          return ListView(
            padding: const EdgeInsets.all(16),
            children: [
              const Text(
                'Khung xuống giống chỉ hướng dẫn. Bạn có thể đổi ngày khi mở vụ.',
              ),
              if (_error != null)
                Text(
                  _error!,
                  style: TextStyle(color: Theme.of(context).colorScheme.error),
                ),
              if (data.calendars.isEmpty) const Text('Chưa có lịch thời vụ.'),
              for (final c in data.calendars)
                Card(
                  child: Padding(
                    padding: const EdgeInsets.all(16),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          '${data.crops[c['crop_code']] ?? c['crop_code']} · ${data.seasons[c['season_code']] ?? c['season_code']}/${c['season_year']}',
                          style: const TextStyle(fontSize: 20),
                        ),
                        Text(
                          'Từ ${_day(c['sow_from'])} đến ${_day(c['sow_to'])}',
                        ),
                        if (widget.calendarRef == null)
                          TextButton(
                            onPressed: () => Navigator.of(context)
                                .push<void>(
                                  MaterialPageRoute(
                                    builder: (_) => SeasonCalendarsScreen(
                                      calendarRef: c['calendar_ref']! as String,
                                    ),
                                  ),
                                )
                                .then((_) {
                                  if (mounted) _reload();
                                }),
                            child: const Text('Xem lịch'),
                          ),
                        if (widget.calendarRef != null &&
                            mode.can(HtxFn.season, HtxAction.create))
                          FilledButton(
                            onPressed: _opening ? null : () => _open(data, c),
                            child: Text(
                              _opening ? 'Đang tải…' : 'Mở vụ theo lịch',
                            ),
                          ),
                      ],
                    ),
                  ),
                ),
            ],
          );
        },
      ),
    );
  }
}
