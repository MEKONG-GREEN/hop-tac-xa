import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/manager_service.dart';
import '../../domain/role_matrix.dart';
import '../../domain/cropcycle.dart';
import '../../domain/roles.dart';
import '../providers.dart';
import 'plots_screen.dart';
import 'household_seasons_screen.dart';
import 'household_profile_screen.dart';

final _householdsProvider = FutureProvider.autoDispose
    .family<List<HouseholdItem>, String?>(
      (ref, coop) => servicesOf(ref).manager.households(cooperativeRef: coop),
    );

String _area(String? value) {
  final scaled = value == null ? null : parseScaled(value, 4);
  return scaled == null ? '—' : formatScaled(scaled, 4, digits: 2);
}

/// Nông hộ của HTX (Đại diện HTX, Thành viên HTX — chỉ xem, FN-04). Chạm → vụ
/// theo hộ; vẫn có lối xem thửa/vụ cũ để ghi nhật ký theo ma trận (HTX US-SEASON-02,
/// US-LOG-02); biểu tượng điện thoại → gọi nhanh, chỉ Đại diện HTX (HTX
/// US-HH-03, SĐT đọc từ máy chủ lúc bấm, không lưu trên máy).
class HouseholdsScreen extends ConsumerStatefulWidget {
  const HouseholdsScreen({super.key});

  @override
  ConsumerState<HouseholdsScreen> createState() => _HouseholdsScreenState();
}

class _HouseholdsScreenState extends ConsumerState<HouseholdsScreen> {
  String? _calling;

  Future<void> _call(HouseholdItem h) async {
    setState(() => _calling = h.ref);
    try {
      await ref.services.manager.callHousehold(h.ref);
    } on ManagerException catch (e) {
      if (mounted) showMessage(context, e.message, error: true);
    } finally {
      if (mounted) setState(() => _calling = null);
    }
  }

  @override
  Widget build(BuildContext context) {
    final list = ref.watch(_householdsProvider(null));
    final mode = ref.watch(appControllerProvider.select((s) => s.mode));
    final canCall =
        mode.callsHouseholds && ref.services.manager.householdCallAvailable;
    return Scaffold(
      appBar: AppBar(title: const Text('Nông hộ')),
      body: list.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (items) => ListView(
          padding: const EdgeInsets.all(8),
          children: [
            if (items.isEmpty)
              const Padding(
                padding: EdgeInsets.all(24),
                child: Text(
                  'Chưa có nông hộ trong HTX đã chọn.',
                  style: TextStyle(fontSize: 17),
                ),
              ),
            for (final h in items)
              Card(
                child: ListTile(
                  leading: const Icon(
                    Icons.home_work,
                    color: AgColors.g700,
                    size: 32,
                  ),
                  title: Text(h.label),
                  subtitle: Text(
                    [
                      if (h.code != null) h.code!,
                      '${h.plots} lô ruộng',
                      'Đất: ${_area(h.landAreaHa)} ha · Đang canh tác: ${_area(h.activeAreaHa)} ha · Còn lại: ${_area(h.remainingAreaHa)} ha',
                      if (!h.areaFresh) 'Số liệu lưu trên máy',
                      if (!h.canOpenSeason) h.blockedReason,
                    ].join('\n'),
                  ),
                  trailing: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      IconButton(
                        tooltip: 'Chi tiết hộ ${h.label}',
                        icon: const Icon(Icons.info_outline),
                        onPressed: () => Navigator.of(context).push(
                          MaterialPageRoute<void>(
                            builder: (_) =>
                                HouseholdProfileScreen(householdRef: h.ref),
                          ),
                        ),
                      ),
                      if (canCall)
                        IconButton(
                          tooltip: 'Gọi ${h.label}',
                          iconSize: 28,
                          color: AgColors.g700,
                          icon: _calling == h.ref
                              ? const SizedBox(
                                  width: 24,
                                  height: 24,
                                  child: CircularProgressIndicator(
                                    strokeWidth: 2,
                                  ),
                                )
                              : const Icon(Icons.phone),
                          onPressed: _calling == null ? () => _call(h) : null,
                        ),
                      const Icon(Icons.chevron_right),
                    ],
                  ),
                  onTap: () => Navigator.of(context)
                      .push(
                        MaterialPageRoute<void>(
                          builder: (_) => HouseholdSeasonsScreen(household: h),
                        ),
                      )
                      .then((_) => ref.invalidate(_householdsProvider(null))),
                ),
              ),
          ],
        ),
      ),
    );
  }
}

final _hhPlotsProvider = FutureProvider.autoDispose
    .family<List<PlotRow>, String>((ref, hh) async {
      final s = servicesOf(ref);
      return [
        for (final p in await s.manager.plotsOf(hh))
          PlotRow(p, await s.crop.openSeasonFor(p.ref)),
      ];
    });

class HouseholdPlotsScreen extends ConsumerWidget {
  const HouseholdPlotsScreen({super.key, required this.household});
  final HouseholdItem household;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final rows = ref.watch(_hhPlotsProvider(household.ref));
    final mode = ref.watch(appControllerProvider.select((s) => s.mode));
    final canOpen = mode.can(HtxFn.season, HtxAction.create);
    final canLog = mode.can(HtxFn.farmLog, HtxAction.create);
    return Scaffold(
      appBar: AppBar(title: Text(household.label)),
      body: rows.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (list) => ListView(
          padding: const EdgeInsets.all(8),
          children: [
            if (list.isEmpty) const Text('Hộ chưa có lô ruộng trên máy.'),
            for (final r in list)
              Card(
                child: ListTile(
                  title: Text(r.plot.title),
                  subtitle: Text(
                    r.season == null && !household.canOpenSeason
                        ? household.blockedReason
                        : r.season == null
                        ? 'Chưa mở vụ${canOpen ? ' — chạm để mở vụ hộ' : ''}'
                        : 'Vụ ${r.season!.seasonCode} ${r.season!.seasonYear}'
                              '${canLog ? ' — chạm để ghi nhật ký hộ' : ''}',
                  ),
                  trailing: const Icon(Icons.chevron_right),
                  onTap: r.season == null && !household.canOpenSeason
                      ? null
                      : () => Navigator.of(context)
                            .push(
                              MaterialPageRoute<void>(
                                builder: (_) =>
                                    PlotDetailScreen(plotRef: r.plot.ref),
                              ),
                            )
                            .then(
                              (_) => ref.invalidate(
                                _hhPlotsProvider(household.ref),
                              ),
                            ),
                ),
              ),
          ],
        ),
      ),
    );
  }
}
