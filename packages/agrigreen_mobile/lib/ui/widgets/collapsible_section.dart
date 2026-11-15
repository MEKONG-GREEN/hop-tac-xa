import 'package:flutter/material.dart';

/// Khối thu/mở dùng chung: luôn đóng khi hiện lần đầu, không có tham số mở sẵn.
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
      maintainState: false,
      title: Text(title, style: Theme.of(context).textTheme.titleMedium),
      children: [child],
    );
  }
}
