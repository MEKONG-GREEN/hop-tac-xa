import 'package:flutter/material.dart';

import '../theme.dart';

/// Một thao tác của hàng nút. Thao tác xóa/hủy đặt `destructive` và chỉ được
/// nằm trong menu "Thêm".
class ActionItem {
  const ActionItem({
    required this.label,
    required this.onPressed,
    this.icon,
    this.destructive = false,
  });
  final String label;
  final VoidCallback onPressed;
  final IconData? icon;
  final bool destructive;
}

/// Hàng nút của thẻ hoặc dòng: một nút chính, tối đa một nút phụ, các thao tác
/// còn lại trong menu "Thêm" (ẩn khi `more` rỗng). Khoảng cách giữa hai nút là 8.
class ActionRow extends StatelessWidget {
  ActionRow({
    super.key,
    required this.primary,
    this.secondary,
    this.more = const [],
  }) : assert(
         !primary.destructive && !(secondary?.destructive ?? false),
         'Xóa/hủy chỉ được nằm trong menu "Thêm"',
       );

  final ActionItem primary;
  final ActionItem? secondary;
  final List<ActionItem> more;

  @override
  Widget build(BuildContext context) {
    final second = secondary;
    return Wrap(
      spacing: 8,
      runSpacing: 8,
      crossAxisAlignment: WrapCrossAlignment.center,
      children: [
        FilledButton(onPressed: primary.onPressed, child: Text(primary.label)),
        if (second != null)
          OutlinedButton(
            onPressed: second.onPressed,
            child: Text(second.label),
          ),
        if (more.isNotEmpty)
          MenuAnchor(
            menuChildren: [
              for (final m in more)
                MenuItemButton(
                  onPressed: m.onPressed,
                  leadingIcon: m.icon == null ? null : Icon(m.icon),
                  style: m.destructive
                      ? MenuItemButton.styleFrom(foregroundColor: AgColors.red)
                      : null,
                  child: Text(m.label),
                ),
            ],
            builder: (context, controller, _) => OutlinedButton.icon(
              onPressed: () =>
                  controller.isOpen ? controller.close() : controller.open(),
              icon: const Icon(Icons.more_horiz),
              label: const Text('Thêm'),
            ),
          ),
      ],
    );
  }
}
