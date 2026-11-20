import 'package:agrigreen_mobile/domain/deep_link.dart';
import 'package:agrigreen_mobile/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../application/app_services.dart';
import '../domain/roles.dart';
import 'providers.dart';
import 'screens/collection_screen.dart';
import 'screens/farm_log_detail_screen.dart';
import 'screens/news_screen.dart';
import 'screens/notifications_screen.dart';
import 'screens/plots_screen.dart';
import 'screens/review_screen.dart';
import 'screens/sync_queue_screen.dart';
import 'screens/season_calendars_screen.dart';

/// Câu khi liên kết dẫn tới màn vai không được xem — cùng câu với
/// `OUT_OF_SCOPE_NOTICE` của web (FN-05/AC-03, EX-01).
const msgNoAccess =
    'Không tìm thấy dữ liệu, hoặc bạn không có quyền xem dữ liệu này.';

/// Màn đích của liên kết theo chế độ, để kiểm quyền trước khi mở.
HtxScreen screenOfLink(HtxMode mode, DeepLink link) => switch (link) {
  FarmLogLink() =>
    mode.canView(HtxScreen.review) ? HtxScreen.review : HtxScreen.farmLogDetail,
  SeasonLink() => HtxScreen.plotDetail,
  CalendarLink() => HtxScreen.seasonCalendars,
  DocumentLink() => HtxScreen.news,
  ConflictLink() => HtxScreen.syncQueue,
  CollectionLink() => HtxScreen.collection,
  DirectiveLink() || TaskLink() || InboxLink() => HtxScreen.notifications,
};

/// Màn hình đích của `deep_link` trong thông báo (lõi `DeepLink.parse`):
/// - `farm_log_reviewed` (nông dân) → nhật ký, có nút "Bổ sung" khi quản lý
///   yêu cầu bổ sung; `farm_log_pending` (quản lý) → hàng chờ duyệt, nhật ký
///   đó lên đầu. Cả hai dùng `/cropcycle/farm-logs/{id}` → chọn theo vai;
/// - `season_opened` (quản lý) → vụ (chi tiết lô của vụ);
/// - `alert` / `document` → bài viết (cảnh báo khẩn cấp HTX US-NEWS-03);
/// - lịch thu gom rơm, bảng kê (`/htx/thu-gom`) → Thu gom rơm MKG;
/// - `sync_conflict` → hàng đợi đồng bộ; còn lại → hộp thư.
///
/// Dữ liệu chưa về máy → màn "chưa có trên máy" có nút đồng bộ lại.
Future<Widget> screenForDeepLink(
  HtxServices s,
  HtxMode mode,
  DeepLink link,
) async {
  switch (link) {
    case CalendarLink(:final ref):
      return SeasonCalendarsScreen(calendarRef: ref);
    case FarmLogLink(:final id):
      return screenOfLink(mode, link) == HtxScreen.review
          ? ReviewScreen(highlightLogId: id)
          : FarmLogDetailScreen(logId: id);
    case SeasonLink(:final ref):
      final season = await s.crop.seasonByRef(ref);
      if (season == null) {
        return const LinkMissingScreen(
          message: 'Vụ này chưa đồng bộ về máy hoặc đã bị hủy.',
        );
      }
      return PlotDetailScreen(plotRef: season.plotRef);
    case DocumentLink(:final ref):
      final a = await s.news.article(ref);
      return a == null
          ? const LinkMissingScreen(
              message: 'Bài viết chưa đồng bộ về máy hoặc đã gỡ.',
            )
          : ArticleScreen(article: a);
    case ConflictLink():
      return const SyncQueueScreen();
    case CollectionLink():
      return const CollectionScreen();
    case DirectiveLink() || TaskLink() || InboxLink():
      return const NotificationsScreen();
  }
}

/// Mở đường dẫn trong app (từ thông báo được chạm hoặc hộp thư). Màn đích
/// không thuộc quyền của vai → về trang chủ theo vai kèm [msgNoAccess]
/// (FN-05/AC-03, EX-01).
Future<void> openDeepLink(
  BuildContext context,
  WidgetRef ref,
  String? raw,
) async {
  final st = ref.read(appControllerProvider);
  final s = st.services;
  if (s == null) return;
  final link = DeepLink.parse(raw);
  if (!st.mode.canView(screenOfLink(st.mode, link))) {
    // SnackBar nằm ở ScaffoldMessenger của MaterialApp nên còn sau khi pop.
    showMessage(context, msgNoAccess);
    Navigator.of(context).popUntil((r) => r.isFirst);
    return;
  }
  final w = await screenForDeepLink(s, st.mode, link);
  if (!context.mounted) return;
  await Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => w));
}

/// Đích của thông báo chưa có trên máy (chưa đồng bộ / hết phạm vi).
class LinkMissingScreen extends ConsumerWidget {
  const LinkMissingScreen({super.key, required this.message});
  final String message;

  @override
  Widget build(BuildContext context, WidgetRef ref) => Scaffold(
    appBar: AppBar(title: const Text('Thông báo')),
    body: Padding(
      padding: const EdgeInsets.all(24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(message, style: const TextStyle(fontSize: 17)),
          const SizedBox(height: 16),
          FilledButton.icon(
            onPressed: () async {
              await ref.services.sync.syncOnce(force: true);
              if (context.mounted) Navigator.of(context).pop();
            },
            icon: const Icon(Icons.sync),
            label: const Text('Đồng bộ ngay'),
          ),
        ],
      ),
    ),
  );
}
