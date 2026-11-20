import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../domain/cropcycle.dart';
import '../providers.dart';

final _profileProvider = FutureProvider.autoDispose
    .family<Map<String, Object?>, String>((ref, household) {
      ref.watch(appControllerProvider.select((state) => state.services));
      ref.watch(appControllerProvider.select((state) => state.mode));
      return servicesOf(ref).manager.householdProfile(household);
    });

/// Native HTX app is read-only for household profiles. The API supplies the
/// Member's masked projection; no raw identity is stored or masked here.
class HouseholdProfileScreen extends ConsumerWidget {
  const HouseholdProfileScreen({super.key, required this.householdRef});
  final String householdRef;
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(_profileProvider(householdRef));
    return Scaffold(
      appBar: AppBar(title: const Text('Chi tiết hộ xã viên')),
      body: state.when(
        skipLoadingOnRefresh: false,
        skipLoadingOnReload: false,
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (_, _) => Center(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Text(
                'Không tải được hồ sơ hộ hoặc bạn không còn quyền truy cập.',
              ),
              TextButton(
                onPressed: () => ref.invalidate(_profileProvider(householdRef)),
                child: const Text('Thử lại'),
              ),
            ],
          ),
        ),
        data: (data) {
          final identity = data['identity'] is Map
              ? data['identity'] as Map
              : const {};
          final household = data['household'] as Map;
          String field(String key) =>
              (identity[key] as String?)?.isNotEmpty == true
              ? identity[key] as String
              : 'Chưa khai báo';
          String area(String key) {
            final value = household[key];
            final scaled = value is String ? parseScaled(value, 4) : null;
            return scaled == null
                ? '—'
                : '${formatScaled(scaled, 4, digits: 4)} ha';
          }

          return ListView(
            padding: const EdgeInsets.all(16),
            children: [
              if (data['contact_access'] == 'masked')
                const Text('SĐT và CCCD được che theo quyền Thành viên HTX.'),
              ListTile(
                title: const Text('Mã hộ'),
                subtitle: Text(data['code'] as String? ?? '—'),
              ),
              ListTile(
                title: const Text('Họ và tên chủ hộ'),
                subtitle: Text(field('full_name')),
              ),
              ListTile(
                title: const Text('Số điện thoại'),
                subtitle: Text(field('phone')),
              ),
              ListTile(
                title: const Text('CCCD'),
                subtitle: Text(field('national_id')),
              ),
              ListTile(
                title: const Text('Địa chỉ'),
                subtitle: Text(field('address')),
              ),
              ListTile(
                title: const Text('Diện tích trồng trọt'),
                subtitle: Text(area('cultivated_area_ha')),
              ),
              ListTile(
                title: const Text('Diện tích chăn nuôi'),
                subtitle: Text(area('livestock_area_ha')),
              ),
              ListTile(
                title: const Text('Trạng thái'),
                subtitle: Text(
                  household['active'] == true
                      ? 'Đang tham gia'
                      : 'Ngừng tham gia',
                ),
              ),
            ],
          );
        },
      ),
    );
  }
}
