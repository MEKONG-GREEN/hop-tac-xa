# Quy ước giao diện chung (App HTX và Web)

Áp cho thành phần dùng chung và chủ đề (theme); không sửa lại từng màn nghiệp vụ. Màn làm mới từ nay dùng các thành phần ở bảng cuối trang. AH-292 (khung web và mẫu màn danh sách) dùng lại `FilterBar` và `RowActions`, không làm bản thứ hai.

## AC1 — App cho nông dân

- Cỡ chữ ≥ 16 cho mọi chữ trong chủ đề và thành phần dùng chung, kể cả chữ phụ của dòng danh sách, nhãn trạng thái và huy hiệu có chữ. Ngoại lệ duy nhất: số đếm trong chấm tròn gắn trên biểu tượng (ví dụ số thông báo chưa đọc).
- Nền sáng; tương phản chữ/nền ≥ 4,5:1. Màu chữ trong `AgColors` (amber, red, blue, mute) đã làm đậm để đạt mức này, nên tối hơn prototype.
- Vùng chạm của nút ≥ 48×48; hai nút kề nhau cách ≥ 8.
- Bố cục vỡ vì chữ to hơn thì sửa bố cục của thành phần dùng chung, không hạ cỡ chữ.
- Kiểm bằng test của chủ đề: `packages/agrigreen_mobile/test/theme_test.dart`.

## AC2 — Hàng nút của thẻ hoặc dòng

- Một nút chính, tối đa một nút phụ, các thao tác còn lại nằm trong menu "Thêm". Màn gọi tự khai báo nút chính, nút phụ (không bắt buộc) và danh sách thao tác cho "Thêm"; thành phần không gắn cứng thao tác nào. Menu "Thêm" không hiện khi không có thao tác nào.
- Mặc định: người dùng có quyền sửa → nút chính "Sửa", nút phụ "Xem". Người dùng chỉ có quyền xem → nút chính "Xem", không có nút phụ.
- Xóa, Ẩn, Hủy, Sao chép và mọi thao tác còn lại nằm trong "Thêm". Xóa và hủy không bao giờ là nút chính hay nút phụ (web: kiểu TypeScript chặn; app: `assert`).

## AC2 — Thanh lọc

- Một hàng: các ô lọc, nút "Lọc thêm" (khi có ô ẩn), số kết quả, nút "Xóa lọc". Số kết quả và "Xóa lọc" luôn hiện.
- Web hiện tối đa 4 ô trên hàng, app tối đa 2 ô; ô còn lại khai báo vào "Lọc thêm". Nút "Lọc thêm" cho biết số ô đang đặt ở phần ẩn: "Lọc thêm (2)".
- "Xóa lọc" bị vô hiệu khi chưa có ô lọc nào được đặt; bấm thì mọi ô về mặc định.
- Màn gọi tự đếm số ô đang đặt và tự đặt lại giá trị trong `onClear`.
- Không cuộn ngang, không giới hạn số ô lọc. Thành phần không tự đo độ rộng để gom ô; trên máy quá hẹp, thanh của app xuống dòng (`Wrap`).

## AC3 — Tạo mới và khối thu/mở

- Thao tác tạo mới mở trang riêng (web) hoặc màn riêng (app), không dùng hộp thoại nổi.
- Khối thu/mở mặc định đóng, dùng `Collapsible` (web) hoặc `CollapsibleSection` (app); các thành phần này không có tham số mở sẵn.
- Trường bắt buộc trong khối đóng vẫn được kiểm khi lưu. Kiểm thất bại thì khối phải mở để người dùng thấy lý do: `Collapsible` (web) tự mở khi ô bên trong không hợp lệ lúc gửi form; với `CollapsibleSection` (app), màn gọi giữ một bộ đếm, tăng nó sau mỗi lần `formKey.currentState!.validate()` trả false và truyền vào `failedChecks`; mỗi lần bộ đếm đổi, khối mở lại, kể cả khi người dùng vừa tự đóng.
- Áp cho màn làm mới từ nay; không đổi các màn cũ.

## AC4 — Thuật ngữ và thiết bị

- Chỉ tiếng Việt, theo [bảng thuật ngữ](thuat-ngu-hien-thi.md). Nhãn điều hướng (mục menu web, ô lối tắt ở màn hình chủ app) và tiêu đề màn tương ứng dùng đúng dạng chuẩn khi chứa thuật ngữ của bảng.
- Không làm bố cục riêng cho máy tính bảng: thành phần dùng chung không thêm nhánh bố cục theo loại thiết bị. Các xử lý theo độ rộng đang có trên web giữ nguyên.

## Thành phần

| Thành phần | Nền tảng | Mã |
|---|---|---|
| `FilterBar`, `FilterChip`, `SearchBox`, `RowActions` | Web | `apps/web-htx/src/components/patterns/list.tsx` |
| `ListScreen`, `MainCell`, `clientList` | Web | `apps/web-htx/src/components/patterns/listScreen.tsx`, `list.tsx` |
| `Collapsible` | Web | `apps/web-htx/src/components/patterns/collapsible.tsx` |
| `buildTheme`, `AgColors` | App | `packages/agrigreen_mobile/lib/ui/theme.dart` |
| `ActionRow`, `ActionItem` | App | `packages/agrigreen_mobile/lib/ui/widgets/action_row.dart` |
| `FilterBar` | App | `packages/agrigreen_mobile/lib/ui/widgets/filter_bar.dart` |
| `CollapsibleSection` | App | `packages/agrigreen_mobile/lib/ui/widgets/collapsible_section.dart` |
