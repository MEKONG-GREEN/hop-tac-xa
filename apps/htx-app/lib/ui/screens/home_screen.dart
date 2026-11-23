import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../domain/role_matrix.dart';
import '../../domain/roles.dart';
import '../navigation.dart';
import '../providers.dart';
import '../widgets.dart';
import 'batch_log_screen.dart';
import 'dashboard_screen.dart';
import 'farm_log_screen.dart';
import 'harvest_screen.dart';
import 'households_screen.dart';
import 'cooperative_info_screen.dart';
import 'bulk_open_screen.dart';
import 'collection_screen.dart';
import 'news_screen.dart';
import 'notifications_screen.dart';
import 'plot_map_screen.dart';
import 'plots_screen.dart';
import 'review_screen.dart';
import 'staff_screen.dart';
import 'support_screen.dart';
import 'season_calendars_screen.dart';
import 'sync_queue_screen.dart';

/// Trang chủ theo vai (HTX US-LOGIN-01 AC-1, FN-05): nông dân, Đại diện HTX
/// hoặc Thành viên HTX.
class HomeScreen extends ConsumerWidget {
  const HomeScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final mode = ref.watch(appControllerProvider.select((s) => s.mode));
    final summary = ref.watch(homeSummaryProvider);
    void open(Widget w) =>
        Navigator.of(context)
            .push(MaterialPageRoute<void>(builder: (_) => w))
            .then((_) => ref.invalidate(homeSummaryProvider));
    final s = summary.value;
    final today = ref.read(appControllerProvider).services?.crop.today();
    final notice = ref.watch(
      appControllerProvider.select((s) => s.services?.workingRole.notice),
    );
    // Thông báo được chạm (kể cả trước khi mở khóa) → mở đúng màn hình.
    void consumeLink(String? link) {
      if (link == null) return;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        final l = ref.read(pendingDeepLinkProvider.notifier).take();
        if (l != null && context.mounted) {
          openDeepLink(
            context,
            ref,
            l,
          ).then((_) => ref.invalidate(homeSummaryProvider));
        }
      });
    }

    ref.listen(pendingDeepLinkProvider, (_, next) => consumeLink(next));
    consumeLink(ref.read(pendingDeepLinkProvider));

    final tiles = switch (mode) {
      HtxMode.farmer => [
        BigTile(
          icon: Icons.grid_view,
          label: 'Ruộng của tôi',
          badge: s == null || s.plots == 0 ? null : '${s.plots}',
          onTap: () => open(const PlotsScreen()),
        ),
        BigTile(
          icon: Icons.edit_note,
          label: 'Ghi nhật ký',
          badge: (s?.needsSupplement ?? 0) > 0
              ? '!${s!.needsSupplement}'
              : null,
          onTap: () => open(const FarmLogScreen()),
        ),
        BigTile(
          icon: Icons.scale,
          label: 'Khai sản lượng',
          onTap: () => open(const HarvestScreen()),
        ),
        BigTile(
          icon: Icons.support_agent,
          label: 'Yêu cầu hỗ trợ',
          onTap: () => open(const SupportScreen()),
        ),
        BigTile(
          icon: Icons.contact_phone_outlined,
          label: 'Danh bạ cán bộ',
          onTap: () => open(const StaffScreen()),
        ),
        BigTile(
          icon: Icons.newspaper,
          label: 'Tin tức',
          onTap: () => open(const NewsScreen()),
        ),
        _queueTile(s, () => open(const SyncQueueScreen())),
      ],
      // Đại diện HTX và Thành viên HTX: một danh sách ô theo thứ tự, lọc theo
      // ma trận quyền (FN-05) — không rải điều kiện theo vai.
      HtxMode.manager || HtxMode.member => [
        for (final (screen, tile) in <(HtxScreen, Widget)>[
          (
            HtxScreen.households,
            BigTile(
              icon: Icons.groups,
              label: 'Nông hộ',
              onTap: () => open(const HouseholdsScreen()),
            ),
          ),
          (
            HtxScreen.plotMap,
            BigTile(
              icon: Icons.map_outlined,
              label: 'Bản đồ lô ruộng',
              onTap: () => open(const PlotMapScreen()),
            ),
          ),
          (
            HtxScreen.seasonCalendars,
            BigTile(
              icon: Icons.calendar_month,
              label: 'Lịch thời vụ',
              onTap: () => open(const SeasonCalendarsScreen()),
            ),
          ),
          (
            HtxScreen.bulkOpen,
            BigTile(
              icon: Icons.playlist_add,
              label: 'Mở vụ hàng loạt',
              onTap: () => open(const BulkOpenScreen()),
            ),
          ),
          (
            HtxScreen.householdLogs,
            BigTile(
              icon: Icons.edit_note,
              label: mode.can(HtxFn.farmLog, HtxAction.create)
                  ? titleWriteHouseholdLogs
                  : titleViewHouseholdLogs,
              onTap: () => open(const PlotsScreen(manager: true)),
            ),
          ),
          (
            HtxScreen.batchLog,
            BigTile(
              icon: Icons.library_add_check_outlined,
              label: 'Nhật ký hàng loạt',
              onTap: () => open(const BatchLogScreen()),
            ),
          ),
          (
            HtxScreen.harvest,
            BigTile(
              icon: Icons.scale,
              label: 'Khai sản lượng',
              onTap: () => open(const HarvestScreen()),
            ),
          ),
          (
            HtxScreen.review,
            BigTile(
              icon: Icons.fact_check_outlined,
              label: 'Duyệt nhật ký',
              badge: (s?.reviewPending ?? 0) > 0 ? '${s!.reviewPending}' : null,
              onTap: () => open(const ReviewScreen()),
            ),
          ),
          (
            HtxScreen.collection,
            BigTile(
              icon: Icons.agriculture,
              label: 'Thu gom rơm MKG',
              onTap: () => open(const CollectionScreen()),
            ),
          ),
          (
            HtxScreen.dashboard,
            BigTile(
              icon: Icons.bar_chart,
              label: 'Tổng quan',
              onTap: () => open(const DashboardScreen()),
            ),
          ),
          (
            HtxScreen.cooperativeInfo,
            BigTile(
              icon: Icons.apartment,
              label: 'Thông tin HTX',
              onTap: () => open(const CooperativeInfoScreen()),
            ),
          ),
          (
            HtxScreen.news,
            BigTile(
              icon: Icons.newspaper,
              label: 'Tin tức',
              onTap: () => open(const NewsScreen()),
            ),
          ),
          (
            HtxScreen.syncQueue,
            _queueTile(s, () => open(const SyncQueueScreen())),
          ),
        ])
          if (mode.canView(screen)) tile,
      ],
      // _AuthGate hiện màn chặn cho hai chế độ này, không vào trang chủ.
      HtxMode.admin || HtxMode.none => const <Widget>[],
    };

    return Scaffold(
      appBar: AppBar(
        title: Text(switch (mode) {
          HtxMode.manager => 'AgriGreen HTX · Quản lý',
          HtxMode.member => 'AgriGreen HTX · Thành viên HTX',
          _ => 'AgriGreen HTX',
        }),
        actions: [
          if ((ref
                          .read(appControllerProvider)
                          .services
                          ?.workingRole
                          .available
                          .length ??
                      0) >
                  1 &&
              ref.read(appControllerProvider).services!.workingRole.enabled)
            IconButton(
              tooltip: 'Đổi vai trò, HTX làm việc',
              icon: const Icon(Icons.switch_account),
              onPressed: () async {
                final confirmed = await showDialog<bool>(
                  context: context,
                  builder: (context) => AlertDialog(
                    content: const Text(
                      'Dữ liệu chưa lưu sẽ mất. Tiếp tục đổi vai trò?',
                    ),
                    actions: [
                      TextButton(
                        onPressed: () => Navigator.pop(context, false),
                        child: const Text('Hủy'),
                      ),
                      FilledButton(
                        onPressed: () => Navigator.pop(context, true),
                        child: const Text('Tiếp tục'),
                      ),
                    ],
                  ),
                );
                if (confirmed == true && context.mounted) {
                  ref
                      .read(appControllerProvider.notifier)
                      .chooseWorkingRoleAgain();
                }
              },
            ),
          IconButton(
            tooltip: 'Thông báo',
            iconSize: 28,
            icon: const Icon(Icons.notifications),
            onPressed: () => open(const NotificationsScreen()),
          ),
          IconButton(
            tooltip: 'Khóa',
            iconSize: 28,
            icon: const Icon(Icons.lock),
            onPressed: () => ref.read(appControllerProvider.notifier).lock(),
          ),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: () async {
          await ref
              .read(appControllerProvider)
              .services
              ?.sync
              .syncOnce(force: true);
          ref.invalidate(homeSummaryProvider);
        },
        child: ListView(
          padding: const EdgeInsets.all(12),
          children: [
            if (notice != null)
              // FN-TK-01/EX-03 (AH-222): vai vừa bị ngừng, còn một vai nên dùng
              // luôn vai đó; giữ câu báo tới khi người dùng đóng.
              Card(
                child: ListTile(
                  leading: const Icon(Icons.info_outline),
                  title: Text(notice),
                  trailing: TextButton(
                    onPressed: () => ref
                        .read(appControllerProvider.notifier)
                        .dismissWorkingRoleNotice(),
                    child: const Text('Đóng'),
                  ),
                ),
              ),
            summary.when(
              data: (s) => SyncCard(summary: s),
              loading: () => const Card(
                child: Padding(
                  padding: EdgeInsets.all(24),
                  child: Center(child: CircularProgressIndicator()),
                ),
              ),
              error: (e, _) => Card(child: ListTile(title: Text('Lỗi: $e'))),
            ),
            const WeatherCard(),
            if (s?.advice != null && today != null)
              AdviceCard(
                season: s!.advice!.$1,
                step: s.advice!.$2,
                today: today,
              ),
            if ((s?.needsSupplement ?? 0) > 0)
              Card(
                color: AgColors.amberBg,
                child: ListTile(
                  leading: const Icon(
                    Icons.assignment_late,
                    color: AgColors.amber,
                  ),
                  title: Text('${s!.needsSupplement} nhật ký cần bổ sung'),
                  subtitle: const Text(
                    'Quản lý HTX yêu cầu bổ sung — chạm để sửa.',
                  ),
                  onTap: () => open(const FarmLogScreen()),
                ),
              ),
            const SizedBox(height: 8),
            GridView.count(
              crossAxisCount: 2,
              shrinkWrap: true,
              physics: const NeverScrollableScrollPhysics(),
              mainAxisSpacing: 10,
              crossAxisSpacing: 10,
              childAspectRatio: 1.3,
              children: tiles,
            ),
          ],
        ),
      ),
    );
  }

  static Widget _queueTile(HomeSummary? s, VoidCallback onTap) => BigTile(
    icon: Icons.cloud_sync_outlined,
    label: 'Hàng đợi đồng bộ',
    badge: s == null
        ? null
        : s.counts.rejected > 0
        ? '!${s.counts.rejected}'
        : s.waiting > 0
        ? '${s.waiting}'
        : null,
    onTap: onTap,
  );
}
