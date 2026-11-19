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

## Trạng thái gói tạo/đọc/sửa hồ sơ

Đã triển khai API và web thêm/sửa hồ sơ, mã HTX tự sinh bất biến, tên duy nhất
theo Unicode trong tenant và nguồn diện tích hộ. Vai Admin thao tác mọi HTX;
Đại diện chỉ sửa HTX mình; Member chỉ đọc và nhận liên hệ đã che từ Vault.
Phản hồi ghi chỉ có ref/code/revision để receipt HTTP không giữ PII.

Sửa dùng ý định `profile_updated` trong sổ Party, command UUIDv7 bền vững ở
Vault và command cập nhật địa chỉ ở Cooperative. Không gọi owner trong
transaction. Thiếu receipt hoàn tất chặn lần sửa mới; client giữ nguyên nội
dung và khóa trong bộ nhớ để retry. Không lưu bản nháp PII vào trình duyệt.
Retry command cũ sau một lần sửa mới không ghi đè thông tin mới. Nếu mất
request gốc khi command chưa hoàn tất thì cần phục hồi thao tác; gói này
chưa cung cấp worker tự phục hồi PII sau khi trình duyệt mất bản nháp.

Liên hệ FN-03 được Vault lưu theo `(tenant, HTX)` với khóa bọc và bản mã
riêng cho từng command. Không dùng SĐT để chống trùng định danh người/hộ:
hai HTX dùng chung SĐT vẫn giữ tên đại diện độc lập. API Vault hẹp kiểm vai
nghiệp vụ cùng org_ref của token; không tự thêm vault_registrar/vault_display
cho người dùng. Member chỉ nhận bản che do chính Vault tạo. Các endpoint
định danh người, sửa nhân sự và quyền Resolve/Unwrap hiện hữu không đổi.
MFA của Admin giữ ở API nền tảng theo phiên đăng nhập hiện hành; không thêm
điều kiện MFA cho Đại diện ngoài ma trận quyền HTX.

Ý định tạo cũng đánh dấu liên hệ chờ hoàn tất, dùng ID sự kiện tạo làm command
bền vững. Khi Vault chưa xong, hồ sơ chưa được trả như một bản đầy đủ thiếu
SĐT. Request tạo lặp khác nội dung bị từ chối bằng digest, không ghi PII vào PF.

Máy móc có port riêng cho AH-260, chưa nối nguồn và hiện `—` theo comment
requirement. Không thay bằng số máy từ nghiệp vụ cơ giới KN.

Kiểm chứng cục bộ: Go build/vet/archlint/modgen/migratecheck/contractcheck/
docscheck/golangci-lint; web check:api/TypeScript/lint, 589 test và build:mock.
Integration thật bao gồm Party/Cooperative cùng pool một kết nối, Vault thật,
lỗi owner giữa hai bước, retry sau khi xóa receipt HTTP, sửa của Đại diện và
chặn HTX khác. Bộ regression FN-06 giữ nguyên 403 khi tài khoản chưa gắn HTX.

**Chưa hoàn thành AH-283:** còn xóa HTX chưa dùng, đếm cả hộ/tài khoản lịch sử,
chặn chia sẻ và dữ liệu nghiệp vụ; phải có chốt ghi đồng thời giữa các owner.
Chưa mở endpoint xóa khi chưa có đủ nguồn kiểm tra. Chỉ gộp gói này sau toàn
bộ CI đúng SHA; giữ story In Progress đến khi đủ AC.

## Thứ tự tiếp theo

Tiếp tục AH-285, nguồn chia sẻ AH-278/AH-274 rồi AH-284/AH-286 trong AH-265.
AH-266 cần nguồn thực tế vật tư/nhật ký, sản lượng, thu gom/nhập kho, hệ số
rơm AH-282 trước AH-287; sau đó AH-288, AH-289/AH-290 và AH-291. Không dùng
liều khuyến cáo SOP thay lượng thực tế, hay diện tích vụ thay diện tích hộ.
