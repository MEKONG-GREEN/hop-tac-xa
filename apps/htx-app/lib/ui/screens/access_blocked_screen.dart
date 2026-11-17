import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../domain/roles.dart';
import '../providers.dart';

/// Câu cho tài khoản Admin đăng nhập app (Admin chỉ dùng web, AH-303).
const msgAdminWebOnly =
    'Tài khoản Admin chỉ dùng trên web. Vui lòng đăng nhập trang quản trị trên máy tính.';

/// Câu cho tài khoản không có vai nào của App HTX (AH-303).
const msgNoHtxRole =
    'Tài khoản không dùng được App HTX. Liên hệ người đã cấp tài khoản.';

/// Màn chặn sau khi mở khóa cho tài khoản Admin và tài khoản không có vai của
/// App HTX: không màn nghiệp vụ nào, không đồng bộ; chỉ có "Đăng xuất".
class AccessBlockedScreen extends ConsumerWidget {
  const AccessBlockedScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final st = ref.watch(appControllerProvider);
    return Scaffold(
      body: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(24),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const Icon(Icons.block, size: 64, color: AgColors.amber),
              const SizedBox(height: 16),
              Text(
                'AgriGreen HTX',
                textAlign: TextAlign.center,
                style: Theme.of(context).textTheme.titleLarge,
              ),
              const SizedBox(height: 16),
              Container(
                key: const ValueKey('blocked-message'),
                padding: const EdgeInsets.all(14),
                decoration: BoxDecoration(
                  color: AgColors.amberBg,
                  borderRadius: BorderRadius.circular(12),
                ),
                child: Text(
                  st.mode == HtxMode.admin ? msgAdminWebOnly : msgNoHtxRole,
                  style: const TextStyle(fontSize: 16),
                ),
              ),
              const SizedBox(height: 24),
              FilledButton(
                key: const ValueKey('blocked-logout'),
                onPressed: st.busy
                    ? null
                    : () => ref.read(appControllerProvider.notifier).signOut(),
                child: const Text('Đăng xuất'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
