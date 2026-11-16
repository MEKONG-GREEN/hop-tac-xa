import 'package:flutter/material.dart';

/// Thanh lọc một hàng: ô lọc (`filters`, quy ước tối đa 2), "Lọc thêm" (các ô
/// trong `moreFilters`), số kết quả và "Xóa lọc".
///
/// Màn gọi tự đếm số ô đang đặt và đặt lại giá trị trong `onClear`. Vừa chỗ thì
/// nằm một hàng; máy quá hẹp thì xuống dòng, không cuộn ngang, không thu nhỏ chữ.
class FilterBar extends StatefulWidget {
  const FilterBar({
    super.key,
    required this.resultCount,
    required this.activeCount,
    required this.onClear,
    required this.filters,
    this.moreFilters = const [],
    this.moreActiveCount = 0,
  });

  final int resultCount;

  /// Số ô lọc đang đặt (cả hàng và phần "Lọc thêm"); 0 → "Xóa lọc" bị vô hiệu.
  final int activeCount;
  final VoidCallback onClear;
  final List<Widget> filters;
  final List<Widget> moreFilters;

  /// Số ô đang đặt trong phần "Lọc thêm".
  final int moreActiveCount;

  @override
  State<FilterBar> createState() => _FilterBarState();
}

class _FilterBarState extends State<FilterBar> {
  bool _moreOpen = false;

  @override
  Widget build(BuildContext context) {
    final hasMore = widget.moreFilters.isNotEmpty;
    final moreLabel = widget.moreActiveCount > 0
        ? 'Lọc thêm (${widget.moreActiveCount})'
        : 'Lọc thêm';
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Wrap(
          spacing: 8,
          runSpacing: 8,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [
            ...widget.filters,
            if (hasMore)
              OutlinedButton.icon(
                onPressed: () => setState(() => _moreOpen = !_moreOpen),
                icon: const Icon(Icons.tune),
                label: Text(moreLabel),
              ),
            Text(
              '${widget.resultCount} kết quả',
              style: const TextStyle(fontSize: 16),
            ),
            TextButton(
              onPressed: widget.activeCount > 0 ? widget.onClear : null,
              child: const Text('Xóa lọc'),
            ),
          ],
        ),
        // Luôn giữ phần ẩn trong cây để ô lọc không mất giá trị đang hiện.
        if (hasMore)
          Visibility(
            visible: _moreOpen,
            maintainState: true,
            child: Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Wrap(
                spacing: 8,
                runSpacing: 8,
                children: widget.moreFilters,
              ),
            ),
          ),
      ],
    );
  }
}
