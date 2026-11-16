import 'package:flutter/material.dart';

/// Khối thu/mở dùng chung: luôn đóng khi hiện lần đầu, không có tham số mở sẵn.
/// Giữ nội dung trong cây widget khi đóng để không mất giá trị đang nhập và
/// để trường bắt buộc trong khối đóng vẫn được `Form.validate()` kiểm.
///
/// Khối đóng thì chữ lỗi của trường bên trong bị ẩn. Màn gọi tăng
/// [failedChecks] sau mỗi lần `validate()` trả false; mỗi lần số này đổi,
/// khối mở ra để người dùng thấy lý do form bị từ chối, kể cả khi họ vừa tự
/// đóng khối sau lần lỗi trước.
class CollapsibleSection extends StatefulWidget {
  const CollapsibleSection({
    super.key,
    required this.title,
    required this.child,
    this.failedChecks = 0,
  });
  final String title;
  final Widget child;

  /// Số lần kiểm form thất bại tính tới giờ.
  final int failedChecks;

  @override
  State<CollapsibleSection> createState() => _CollapsibleSectionState();
}

class _CollapsibleSectionState extends State<CollapsibleSection> {
  final _controller = ExpansibleController();

  @override
  void didUpdateWidget(CollapsibleSection oldWidget) {
    super.didUpdateWidget(oldWidget);
    final failedAgain = widget.failedChecks != oldWidget.failedChecks;
    if (failedAgain && !_controller.isExpanded) {
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
      maintainState: true,
      title: Text(widget.title, style: Theme.of(context).textTheme.titleMedium),
      children: [widget.child],
    );
  }
}
