import 'package:agrigreen_mobile/application/inbox_service.dart';
import 'package:agrigreen_mobile/domain/deep_link.dart';
import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../navigation.dart';
import '../providers.dart';

final _inboxProvider = FutureProvider.autoDispose<List<InboxItem>>(
  (ref) => servicesOf(ref).inbox.list(),
);

/// Trung tâm thông báo (entity `notification`, 30 ngày): nhóm theo ngày (giờ
/// Việt Nam), trạng thái đọc, bộ lọc chưa đọc, "Đọc tất cả" (UX-08, AH-22).
/// Đánh dấu đã đọc xếp hàng khi ngoại tuyến; chạm → đánh dấu đã đọc rồi mở đúng
/// màn hình theo `deep_link` (navigation.dart). Thông báo chưa có màn đích (lịch
/// thời vụ) ở lại trung tâm và ghi "Chưa có màn hình liên kết".
class NotificationsScreen extends ConsumerStatefulWidget {
  const NotificationsScreen({super.key});

  @override
  ConsumerState<NotificationsScreen> createState() =>
      _NotificationsScreenState();
}

class _NotificationsScreenState extends ConsumerState<NotificationsScreen> {
  bool _unreadOnly = false;

  Future<void> _readAll() async {
    await ref.services.inbox.markAllRead();
    ref.invalidate(_inboxProvider);
  }

  @override
  Widget build(BuildContext context) {
    final list = ref.watch(_inboxProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('Thông báo'),
        actions: [
          TextButton(onPressed: _readAll, child: const Text('Đọc tất cả')),
        ],
      ),
      body: list.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text('Lỗi: $e')),
        data: (all) {
          final items = _unreadOnly ? all.where((n) => !n.read).toList() : all;
          final days = groupInboxByDay(
            items,
            ref.read(platformProvider).clock.now(),
          );
          return ListView(
            padding: const EdgeInsets.all(8),
            children: [
              Align(
                alignment: Alignment.centerLeft,
                child: FilterChip(
                  label: const Text('Chỉ chưa đọc'),
                  selected: _unreadOnly,
                  onSelected: (v) => setState(() => _unreadOnly = v),
                ),
              ),
              if (days.isEmpty)
                const Padding(
                  padding: EdgeInsets.all(24),
                  child: Center(
                    child: Text(
                      'Chưa có thông báo.',
                      style: TextStyle(fontSize: 17),
                    ),
                  ),
                ),
              for (final day in days) ...[
                Padding(
                  padding: const EdgeInsets.fromLTRB(8, 12, 8, 4),
                  child: Text(
                    day.label,
                    style: const TextStyle(
                      fontSize: 15,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ),
                for (final n in day.items) _NoticeTile(item: n, onOpen: _open),
              ],
            ],
          );
        },
      ),
    );
  }

  Future<void> _open(InboxItem n) async {
    await ref.services.inbox.markRead(n.id);
    ref.invalidate(_inboxProvider);
    if (n.link is InboxLink || !mounted) return;
    await openDeepLink(context, ref, n.deepLink);
  }
}

class _NoticeTile extends StatelessWidget {
  const _NoticeTile({required this.item, required this.onOpen});

  final InboxItem item;
  final Future<void> Function(InboxItem) onOpen;

  @override
  Widget build(BuildContext context) {
    final n = item;
    final t = n.createdAt;
    return Card(
      child: ListTile(
        leading: Icon(
          n.severity == 'urgent' ? Icons.warning_amber : Icons.notifications,
          color: n.severity == 'urgent' ? AgColors.red : AgColors.g700,
        ),
        title: Text(
          n.title,
          style: TextStyle(
            fontWeight: n.read ? FontWeight.w400 : FontWeight.w700,
          ),
        ),
        subtitle: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(n.body),
            Text(
              '${t == null ? '—' : inboxTime(t)} · ${n.read ? 'Đã đọc' : 'Chưa đọc'}',
              key: Key('read-state-${n.id}'),
            ),
            if (n.link is InboxLink) const Text('Chưa có màn hình liên kết'),
          ],
        ),
        onTap: () => onOpen(n),
      ),
    );
  }
}
