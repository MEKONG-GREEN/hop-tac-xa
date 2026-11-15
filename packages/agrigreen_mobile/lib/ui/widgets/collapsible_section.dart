import 'package:flutter/material.dart';

/// Khối thu/mở dùng chung: luôn đóng khi hiện lần đầu, không có tham số mở sẵn.
/// Giữ nội dung trong cây widget khi đóng để không mất giá trị đang nhập và
/// để trường bắt buộc trong khối đóng vẫn được `Form.validate()` kiểm.
class CollapsibleSection extends StatelessWidget {
  const CollapsibleSection({
    super.key,
    required this.title,
    required this.child,
  });
  final String title;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    return ExpansionTile(
      initiallyExpanded: false,
      maintainState: true,
      title: Text(title, style: Theme.of(context).textTheme.titleMedium),
      children: [child],
    );
  }
}
