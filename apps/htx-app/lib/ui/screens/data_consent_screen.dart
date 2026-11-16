import 'package:agrigreen_mobile/ui/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers.dart';

/// Đồng ý thu thập ảnh gắn GPS và dữ liệu sản xuất (AH-23, REQ-011, KH-24):
/// hiện sau khi mở khóa, trước khi vào ứng dụng. "Không đồng ý" → đăng xuất
/// (lần đăng nhập sau hỏi lại). Chưa xác nhận được trạng thái (lỗi mạng/máy
/// chủ) → màn chặn: chỉ "Thử lại" hoặc "Đăng xuất".
class DataConsentScreen extends ConsumerWidget {
  const DataConsentScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final st = ref.watch(appControllerProvider);
    final consent = st.consent;
    final ctl = ref.read(appControllerProvider.notifier);
    final prompt = consent != null && consent.isPrompt;
    return Scaffold(
      body: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(24),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const Icon(Icons.privacy_tip, size: 64, color: AgColors.g700),
              const SizedBox(height: 16),
              Text(
                'Đồng ý thu thập dữ liệu',
                textAlign: TextAlign.center,
                style: Theme.of(context).textTheme.titleLarge,
              ),
              const SizedBox(height: 16),
              if (prompt) ...[
                Text(consent.text!, style: const TextStyle(fontSize: 16)),
                const SizedBox(height: 12),
                Text(
                  'Phiên bản nội dung: ${consent.version}',
                  style: const TextStyle(fontSize: 14, color: AgColors.mute),
                ),
              ],
              if (consent?.message != null) ...[
                const SizedBox(height: 16),
                Container(
                  key: const ValueKey('consent-message'),
                  padding: const EdgeInsets.all(14),
                  decoration: BoxDecoration(
                    color: AgColors.amberBg,
                    borderRadius: BorderRadius.circular(12),
                  ),
                  child: Text(
                    consent!.message!,
                    style: const TextStyle(fontSize: 16),
                  ),
                ),
              ],
              const SizedBox(height: 24),
              if (prompt) ...[
                FilledButton(
                  key: const ValueKey('consent-accept'),
                  onPressed: st.busy ? null : () => ctl.acceptDataConsent(),
                  child: const Text('Đồng ý'),
                ),
                const SizedBox(height: 12),
                OutlinedButton(
                  key: const ValueKey('consent-decline'),
                  onPressed: st.busy ? null : () => ctl.declineDataConsent(),
                  child: const Text('Không đồng ý'),
                ),
              ] else ...[
                FilledButton(
                  key: const ValueKey('consent-retry'),
                  onPressed: st.busy ? null : () => ctl.retryDataConsent(),
                  child: const Text('Thử lại'),
                ),
                const SizedBox(height: 12),
                OutlinedButton(
                  key: const ValueKey('consent-logout'),
                  onPressed: st.busy ? null : () => ctl.declineDataConsent(),
                  child: const Text('Đăng xuất'),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}
