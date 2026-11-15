import 'package:flutter/material.dart';

/// Khối thu/mở dùng chung: đóng khi hiện lần đầu, không có tham số mở sẵn.
/// Giữ nội dung trong cây widget khi đóng để không mất giá trị đang nhập và
/// để trường bắt buộc trong khối đóng vẫn được `Form.validate()` kiểm.
///
/// Khối đóng thì chữ lỗi của trường bên trong bị ẩn, nên màn gọi truyền
/// [hasError] (ví dụ `!formKey.currentState!.validate()`): khi cờ bật, khối
/// tự mở để người dùng thấy lý do form bị từ chối.
class CollapsibleSection extends StatefulWidget {
  const CollapsibleSection({
    super.key,
    required this.title,
    required this.child,
    this.hasError = false,
  });
  final String title;
  final Widget child;
  final bool hasError;

  @override
  State<CollapsibleSection> createState() => _CollapsibleSectionState();
}

class _CollapsibleSectionState extends State<CollapsibleSection> {
  final _controller = ExpansibleController();

  @override
  void didUpdateWidget(CollapsibleSection oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.hasError && !oldWidget.hasError && !_controller.isExpanded) {
      _controller.expand();
    }
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return ExpansionTile(
      controller: _controller,
      initiallyExpanded: widget.hasError,
      maintainState: true,
      title: Text(widget.title, style: Theme.of(context).textTheme.titleMedium),
      children: [widget.child],
    );
  }
}
