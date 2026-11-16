import 'package:flutter/material.dart';

import '../../domain/outbox_record.dart';
import '../theme.dart';

/// Ô lối tắt lớn trên màn hình chủ.
class BigTile extends StatelessWidget {
  const BigTile({
    super.key,
    required this.icon,
    required this.label,
    required this.onTap,
    this.badge,
  });
  final IconData icon;
  final String label;
  final VoidCallback onTap;
  final String? badge;

  @override
  Widget build(BuildContext context) {
    return Material(
      color: Colors.white,
      borderRadius: BorderRadius.circular(16),
      child: InkWell(
        borderRadius: BorderRadius.circular(16),
        onTap: onTap,
        child: Container(
          padding: const EdgeInsets.all(8),
          // Ô thấp (Android giá rẻ): chỉ biểu tượng co lại, chữ nhãn giữ cỡ 17.
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              Flexible(
                child: FittedBox(
                  fit: BoxFit.scaleDown,
                  child: Stack(
                    clipBehavior: Clip.none,
                    children: [
                      Icon(icon, size: 40, color: AgColors.g700),
                      if (badge != null)
                        Positioned(
                          right: -14,
                          top: -8,
                          child: Container(
                            padding: const EdgeInsets.symmetric(
                              horizontal: 7,
                              vertical: 2,
                            ),
                            decoration: BoxDecoration(
                              color: AgColors.red,
                              borderRadius: BorderRadius.circular(12),
                            ),
                            // Số đếm trong chấm tròn là ngoại lệ duy nhất của chữ ≥ 16.
                            child: Text(
                              badge!,
                              style: const TextStyle(
                                color: Colors.white,
                                fontSize: 14,
                                fontWeight: FontWeight.w700,
                              ),
                            ),
                          ),
                        ),
                    ],
                  ),
                ),
              ),
              const SizedBox(height: 4),
              Text(
                label,
                textAlign: TextAlign.center,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: const TextStyle(
                  fontSize: 17,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// Màu nền và màu chữ của nhãn trạng thái hàng đợi (tách ra để test tương phản).
(Color bg, Color fg) statusChipColors(OutboxStatus status) => switch (status) {
  OutboxStatus.pending => (AgColors.amberBg, AgColors.amber),
  OutboxStatus.sent => (AgColors.g100, AgColors.g700),
  OutboxStatus.superseded => (AgColors.g100, AgColors.g700),
  OutboxStatus.conflict => (AgColors.amberBg, AgColors.amber),
  OutboxStatus.rejected => (AgColors.redBg, AgColors.red),
};

class StatusChip extends StatelessWidget {
  const StatusChip(this.status, {super.key});
  final OutboxStatus status;

  @override
  Widget build(BuildContext context) {
    final (bg, fg) = statusChipColors(status);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
      decoration: BoxDecoration(
        color: bg,
        borderRadius: BorderRadius.circular(20),
      ),
      child: Text(
        status.label,
        style: TextStyle(color: fg, fontSize: 16, fontWeight: FontWeight.w600),
      ),
    );
  }
}

void showMessage(BuildContext context, String text, {bool error = false}) {
  ScaffoldMessenger.of(context).showSnackBar(
    SnackBar(
      content: Text(text, style: const TextStyle(fontSize: 16)),
      backgroundColor: error ? AgColors.red : AgColors.g900,
    ),
  );
}

String entityLabel(String entity) => switch (entity) {
  PushEntity.plotBoundary => 'Ranh giới thửa',
  PushEntity.knPlotBoundary => 'Ranh giới thửa',
  PushEntity.fieldReport => 'Báo cáo hiện trường',
  PushEntity.taskUpdate => 'Cập nhật nhiệm vụ',
  PushEntity.consent => 'Đồng ý của nông hộ',
  PushEntity.evidence => 'Ảnh minh chứng',
  PushEntity.seasonOpen => 'Mở vụ',
  PushEntity.farmLog => 'Nhật ký canh tác',
  PushEntity.farmLogBatch => 'Nhật ký hàng loạt',
  PushEntity.harvest => 'Khai báo sản lượng',
  PushEntity.supportRequest => 'Yêu cầu hỗ trợ',
  PushEntity.rollCount => 'Đếm cuộn',
  PushEntity.htxCollectionResponse => 'Phản hồi bảng kê thu gom',
  _ => entity,
};

String formatLocal(DateTime? t) {
  if (t == null) return '—';
  final l = t.toLocal();
  String two(int v) => v.toString().padLeft(2, '0');
  return '${two(l.hour)}:${two(l.minute)} ${two(l.day)}/${two(l.month)}/${l.year}';
}
