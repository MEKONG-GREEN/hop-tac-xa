import 'package:agrigreen_mobile/data/db/local_db.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

/// Khóa trong bảng meta: Thành viên HTX đã xem hết hoặc bỏ qua hướng dẫn (AH-101).
const onboardingMetaKey = 'htx.onboarding.v1';

/// Tự mở hướng dẫn khi Thành viên HTX vào trang chủ lần đầu. Kiểm thử widget
/// tắt mặc định để các luồng khác không bị màn hướng dẫn che; kiểm thử của
/// AH-101 bật lại.
final onboardingAutoShowProvider = Provider<bool>((ref) => true);

/// Một bước hướng dẫn: hình minh họa, tiêu đề, lời dẫn ngắn.
class OnboardingStep {
  const OnboardingStep(this.icon, this.title, this.body);
  final IconData icon;
  final String title;
  final String body;
}

/// Các bước của Thành viên HTX (UX-03): mở vụ, ghi nhật ký, khai báo sản lượng.
/// Hình minh họa tạm dùng biểu tượng lớn; thay bằng hình của prototype khi có
/// tài liệu (KH-31).
const memberOnboardingSteps = [
  OnboardingStep(
    Icons.agriculture,
    'Mở vụ cho nông hộ',
    'Vào "Nông hộ", chọn hộ rồi bấm "Mở vụ mới". Chọn loại cây, giống, '
        'diện tích và ngày xuống giống. Quy trình (SOP) sẽ tự hiện lịch việc.',
  ),
  OnboardingStep(
    Icons.checklist,
    'Ghi nhật ký bằng một chạm',
    'Mở vụ để xem danh sách việc. Làm xong việc nào thì tích vào việc đó. '
        'Cần ghi liều, ảnh hay ngày khác thì bấm "Ghi chi tiết". '
        'Mất mạng vẫn ghi được, máy tự gửi khi có mạng.',
  ),
  OnboardingStep(
    Icons.scale,
    'Khai báo sản lượng',
    'Thu hoạch xong, mở vụ và nhập sản lượng thực tế. '
        'Kiểm lại số trước khi lưu; sai thì sửa kèm lý do.',
  ),
];

/// Đọc cờ đã xem hướng dẫn.
Future<bool> onboardingSeen(LocalDb db) async =>
    await db.getMeta(onboardingMetaKey) != null;

/// Ghi cờ đã xem (xem hết hoặc bỏ qua) — không hiện lại lần sau (AC3).
Future<void> markOnboardingSeen(LocalDb db, String how) =>
    db.setMeta(onboardingMetaKey, how);

/// Màn hướng dẫn từng bước (AH-101, UX-03): chữ ≥ 18sp, nút ≥ 48×48dp; bỏ qua
/// được; xem lại từ nút "Hướng dẫn" trên thanh trên cùng của trang chủ.
class OnboardingScreen extends StatefulWidget {
  const OnboardingScreen({
    super.key,
    required this.onFinished,
    this.steps = memberOnboardingSteps,
  });

  /// Gọi khi xem hết ("done") hoặc bỏ qua ("skipped").
  final Future<void> Function(String how) onFinished;
  final List<OnboardingStep> steps;

  @override
  State<OnboardingScreen> createState() => _OnboardingScreenState();
}

class _OnboardingScreenState extends State<OnboardingScreen> {
  final _pages = PageController();
  var _index = 0;
  var _closing = false;

  @override
  void dispose() {
    _pages.dispose();
    super.dispose();
  }

  Future<void> _finish(String how) async {
    if (_closing) return;
    _closing = true;
    await widget.onFinished(how);
    if (mounted) Navigator.of(context).pop();
  }

  @override
  Widget build(BuildContext context) {
    final last = _index == widget.steps.length - 1;
    final theme = Theme.of(context);
    const big = Size(48, 48);
    return Scaffold(
      appBar: AppBar(
        title: const Text('Hướng dẫn sử dụng'),
        automaticallyImplyLeading: false,
        actions: [
          TextButton(
            style: TextButton.styleFrom(
              minimumSize: big,
              textStyle: const TextStyle(fontSize: 18),
            ),
            onPressed: () => _finish('skipped'),
            child: const Text('Bỏ qua'),
          ),
        ],
      ),
      body: SafeArea(
        child: Column(
          children: [
            Expanded(
              child: PageView(
                controller: _pages,
                onPageChanged: (i) => setState(() => _index = i),
                children: [
                  for (final s in widget.steps)
                    SingleChildScrollView(
                      padding: const EdgeInsets.all(24),
                      child: Column(
                        children: [
                          Icon(
                            s.icon,
                            size: 120,
                            color: theme.colorScheme.primary,
                            semanticLabel: s.title,
                          ),
                          const SizedBox(height: 24),
                          Text(
                            s.title,
                            textAlign: TextAlign.center,
                            style: const TextStyle(
                              fontSize: 24,
                              fontWeight: FontWeight.bold,
                            ),
                          ),
                          const SizedBox(height: 16),
                          Text(
                            s.body,
                            textAlign: TextAlign.center,
                            style: const TextStyle(fontSize: 18, height: 1.4),
                          ),
                        ],
                      ),
                    ),
                ],
              ),
            ),
            Text(
              'Bước ${_index + 1}/${widget.steps.length}',
              style: const TextStyle(fontSize: 16),
            ),
            Padding(
              padding: const EdgeInsets.all(16),
              child: Row(
                children: [
                  if (_index > 0)
                    OutlinedButton(
                      style: OutlinedButton.styleFrom(
                        minimumSize: big,
                        textStyle: const TextStyle(fontSize: 18),
                      ),
                      onPressed: () => _pages.previousPage(
                        duration: const Duration(milliseconds: 250),
                        curve: Curves.easeOut,
                      ),
                      child: const Text('Quay lại'),
                    ),
                  const Spacer(),
                  FilledButton(
                    style: FilledButton.styleFrom(
                      minimumSize: const Size(120, 48),
                      textStyle: const TextStyle(fontSize: 18),
                    ),
                    onPressed: last
                        ? () => _finish('done')
                        : () => _pages.nextPage(
                            duration: const Duration(milliseconds: 250),
                            curve: Curves.easeOut,
                          ),
                    child: Text(last ? 'Bắt đầu dùng' : 'Tiếp'),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// Mở màn hướng dẫn; xem hết hoặc bỏ qua thì ghi cờ để không tự hiện lại.
Future<void> openOnboarding(BuildContext context, LocalDb db) =>
    Navigator.of(context).push(
      MaterialPageRoute<void>(
        fullscreenDialog: true,
        builder: (_) =>
            OnboardingScreen(onFinished: (how) => markOnboardingSeen(db, how)),
      ),
    );

/// Hiện hướng dẫn một lần khi vào trang chủ lần đầu (AC1, AC3).
Future<void> maybeShowOnboarding(
  BuildContext context,
  WidgetRef ref,
  LocalDb db,
) async {
  if (!ref.read(onboardingAutoShowProvider)) return;
  if (await onboardingSeen(db)) return;
  if (!context.mounted) return;
  await openOnboarding(context, db);
}
