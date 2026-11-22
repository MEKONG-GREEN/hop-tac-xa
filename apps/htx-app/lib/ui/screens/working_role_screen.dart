import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../domain/roles.dart';
import '../providers.dart';

/// Tên HTX làm việc (cả hai vai cùng một HTX, FN-07/BR-02). Bản đệm trước,
/// thiếu thì thử tải `/v1/me`; lỗi mạng chỉ làm thẻ không có tên HTX.
final _htxNameProvider = FutureProvider.autoDispose<String?>((ref) async {
  final s = servicesOf(ref);
  final cached = await s.profile.cooperativeName();
  if (cached != null) return cached;
  try {
    await s.profile.refresh(s.api);
  } catch (_) {
    return null;
  }
  return s.profile.cooperativeName();
});

class WorkingRoleScreen extends ConsumerStatefulWidget {
  const WorkingRoleScreen({super.key});
  @override
  ConsumerState<WorkingRoleScreen> createState() => _WorkingRoleScreenState();
}

class _WorkingRoleScreenState extends ConsumerState<WorkingRoleScreen> {
  bool busy = false;
  String? error;

  Future<void> choose(String role) async {
    setState(() {
      busy = true;
      error = null;
    });
    try {
      await ref.read(appControllerProvider.notifier).selectWorkingRole(role);
    } catch (failure) {
      if (mounted) {
        setState(() {
          busy = false;
          error = failure is StateError
              ? failure.message
              : 'Chưa chọn được vai trò. Vui lòng thử lại.';
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final selection = ref.watch(appControllerProvider).services!.workingRole;
    final htxName = ref.watch(_htxNameProvider).value;
    return Scaffold(
      appBar: AppBar(title: const Text('Chọn vai trò và HTX làm việc')),
      body: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const Text('Mỗi lần chỉ dùng quyền của một vai trong HTX của bạn.'),
            if (error != null) Text(error!),
            for (final role in selection.available)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 8),
                child: FilledButton(
                  onPressed: busy ? null : () => choose(role),
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Text(
                        role == HtxRole.coopManager
                            ? 'Đại diện HTX'
                            : 'Thành viên HTX',
                      ),
                      if (htxName != null) Text(htxName),
                    ],
                  ),
                ),
              ),
          ],
        ),
      ),
    );
  }
}
