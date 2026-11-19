# AH-283 — Hồ sơ HTX

Đích tích hợp: `dev-agent`. AH-51 đã hoàn thành trước khi bắt đầu; danh mục
ĐVHC AH-271 phải có trên nhánh đích trước khi gộp hồ sơ. Truy vết theo BRD
Quản trị/phân quyền v3.0 FN-03 và comment Jira ngày 15/11/2026; giữ mã AC-07
như tài liệu gốc. Phần danh sách/chia sẻ theo vai thuộc AH-284, không nghiệm
thu thay bằng màn KN cũ.

## Các gói triển khai

1. Dùng một quy tắc tên HTX duy nhất trong tenant cho tạo, nhập, sao chép,
   đổi tên; khóa ghi đồng thời bằng khóa Unicode đã chuẩn hóa. Không phụ thuộc
   locale PostgreSQL. Tên HTX ngừng hoạt động vẫn được giữ; mã hệ thống bất biến.
2. Bổ sung diện tích chăn nuôi vào nguồn hồ sơ hộ của `pf/cooperative`;
   kiểm số thập phân ở server và giữ các trường cũ/lịch sử. Đọc tổng diện tích
   từ module sở hữu qua port, không sao chép thành trường nhập tay của HTX.
3. Thêm API hồ sơ HTX cho Admin và Đại diện: kiểm vai và HTX sở hữu trước khi
   chạm Vault; Member chỉ đọc. Tạo HTX tiếp tục lõi Registry/CoopProfiles hai
   bước, sửa có sổ bù và retry cùng khóa. Địa giới dùng AH-271; không yêu cầu
   mã số thuế hay neo KN cho luồng hồ sơ HTX mới. Không mở lại đường tạo HTX
   của API tổ chức chung.
4. Xóa HTX tạo nhầm: lấy số hộ/tài khoản **kể cả lịch sử và ngừng/khóa**, yêu
   cầu chia sẻ và dữ liệu nghiệp vụ từ nguồn thật. Thiếu nguồn kiểm tra phải
   chặn. Thao tác chỉ Admin, xác nhận trên web, không sửa/xóa sổ audit; dữ liệu
   chiếu bị xóa không được sống lại khi rebuild hoặc retry yêu cầu tạo cũ.
5. Web dùng mẫu danh sách/form hiện có, nhãn số điện thoại và đại diện chỉ qua
   Vault. Diện tích chỉ đọc. Máy móc phải lấy tổng số lượng từ AH-260 với nhóm
   danh mục động; chưa có nguồn hiển thị `—`, không dùng số máy cơ giới KN thay thế.

## Kiểm chứng bắt buộc

- Tên tiếng Việt khác hoa/thường, khoảng trắng và biểu diễn Unicode; tạo/đổi
  tên trùng không ghi sổ; hai yêu cầu đồng thời chỉ một thành công; tenant độc lập.
- Admin mọi HTX, Đại diện chỉ sửa HTX mình, Member chỉ đọc; đường dẫn/API trực
  tiếp không vượt quyền, kể cả vai kiêm. Không đưa PII vào bảng nghiệp vụ/log.
- Thêm hộ 1,5 ha trồng trọt và 0,5 ha chăn nuôi: tổng đúng HTX đó, không tăng
  HTX khác; số liệu nullable cũ không được ngầm coi là đã khai báo.
- Xóa HTX với 3 hộ/2 tài khoản trả số lượng chính xác; lịch sử tài khoản, hộ
  ngừng tham gia và chia sẻ cũ đều chặn; HTX mới chưa dùng xóa được, không hồi sinh.
- Toàn bộ cổng Go/web/Flutter và CI ở đúng SHA phải xanh trước merge. AH-283
  chỉ hoàn thành khi đủ AC; nền tên duy nhất riêng lẻ không phải hoàn thành story.

## Sau AH-283

Tiếp tục AH-285, nguồn chia sẻ AH-278/AH-274 rồi AH-284/AH-286 trong AH-265.
AH-266 cần nguồn thực tế vật tư/nhật ký, sản lượng, thu gom/nhập kho, hệ số
rơm AH-282 trước AH-287; sau đó AH-288, AH-289/AH-290 và AH-291. Không dùng
liều khuyến cáo SOP thay lượng thực tế, hay diện tích vụ thay diện tích hộ.
