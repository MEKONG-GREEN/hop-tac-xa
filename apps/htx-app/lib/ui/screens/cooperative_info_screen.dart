import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../application/cooperative_info.dart';
import '../../domain/cropcycle.dart';
import '../providers.dart';

final _cooperativeInfoProvider = FutureProvider.autoDispose<CooperativeInfo>(
  (ref) => CooperativeInfoService(
    api: servicesOf(ref).api,
    db: servicesOf(ref).db,
    profile: servicesOf(ref).profile,
  ).load(),
);

String _ha(String? value) {
  final scaled = value == null ? null : parseScaled(value, 4);
  return scaled == null ? '—' : '${formatScaled(scaled, 4, digits: 2)} ha';
}

/// Thông tin HTX của mình (FN-03, AH-284) — Đại diện HTX và Thành viên HTX,
/// chỉ xem. Sửa HTX, liên kết và chia sẻ dữ liệu chỉ có trên web (PO duyệt
/// 15/11/2026).
class CooperativeInfoScreen extends ConsumerWidget {
  const CooperativeInfoScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final info = ref.watch(_cooperativeInfoProvider);
    return Scaffold(
      appBar: AppBar(title: const Text('Thông tin HTX')),
      body: info.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(
          child: Padding(
            padding: const EdgeInsets.all(24),
            child: Text('$e', style: const TextStyle(fontSize: 17)),
          ),
        ),
        data: (c) => RefreshIndicator(
          onRefresh: () => ref.refresh(_cooperativeInfoProvider.future),
          child: ListView(
            padding: const EdgeInsets.all(12),
            children: [
              if (!c.fresh)
                const Card(
                  child: ListTile(
                    leading: Icon(Icons.cloud_off),
                    title: Text('Đang xem thông tin đã lưu (chưa có mạng).'),
                  ),
                ),
              Card(
                child: ListTile(
                  leading: const Icon(
                    Icons.apartment,
                    color: AgColors.g700,
                    size: 36,
                  ),
                  title: Text(
                    c.name,
                    style: const TextStyle(
                      fontSize: 19,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                  subtitle: Text(
                    c.active ? 'Đang hoạt động' : 'Đã vô hiệu hoá',
                    style: const TextStyle(fontSize: 15),
                  ),
                ),
              ),
              Card(
                child: Column(
                  children: [
                    _Row('Mã HTX', c.code),
                    if (c.taxCode != null) _Row('Mã số thuế', c.taxCode!),
                    _Row('Địa bàn', c.place ?? '—'),
                    _Row('Hộ thành viên', c.households?.toString() ?? '—'),
                    _Row('Diện tích trồng trọt', _ha(c.cultivatedHa)),
                    _Row('Diện tích chăn nuôi', _ha(c.livestockHa)),
                    _Row('Máy móc hiện có', c.machineCount?.toString() ?? '—'),
                  ],
                ),
              ),
              const Padding(
                padding: EdgeInsets.all(8),
                child: Text(
                  'Chỉ xem. Sửa thông tin HTX trên web.',
                  style: TextStyle(fontSize: 15, color: Colors.black54),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _Row extends StatelessWidget {
  const _Row(this.label, this.value);
  final String label;
  final String value;

  @override
  Widget build(BuildContext context) => ListTile(
    dense: false,
    title: Text(label, style: const TextStyle(fontSize: 15)),
    trailing: Text(
      value,
      style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w600),
    ),
  );
}
