# AH-285 — hộ xã viên theo luồng HTX hiện tại

Nhánh đích: `dev-agent`. AH-51 đã hoàn tất; AH-283 là dependency hồ sơ HTX.
Requirement đối chiếu comment Jira ngày 17/11/2026 và ma trận FN-04 dùng chung.

## Phần đang triển khai

Nền tảng quyền đọc định danh hộ đã có mã và kiểm thử, chưa phải toàn bộ story:

- Cooperative xác minh membership bằng token gốc tại hai GET metadata
  `/v1/households/:ref/identity-scope` và
  `/v1/cooperatives/:ref/household-identity-scope`. Không trả PII, không gọi Vault.
- Vault có POST `/v1/household-identities/:ref/read`, chỉ mục đích
  `extension.contact`. Mỗi lần đọc lấy bằng chứng owner trước và sau giải mã;
  không giữ transaction khi gọi owner hoặc KMS. Ghi audit trước khi trả dữ liệu.
- Member chỉ nhận SĐT giữ 4 số đầu/3 số cuối và CCCD giữ 3 số đầu/3 số cuối.
  Đại diện chỉ đọc hộ thuộc HTX mình; Admin trong tenant, bắt buộc MFA.
  Vai KN đi kèm không mở rộng phạm vi HTX của Đại diện/Member.
- Chỉ chủ thể `household`; không mở API này cho định danh `person`.
  Không cấp thêm `vault_registrar`, không thay quyền API Resolve dùng chung.
- HTTP không theo redirect; lỗi owner không phản chiếu payload về caller.
  Phản hồi định danh và metadata đều `Cache-Control: no-store`.

## Cấu hình

`AG_VAULT_HOUSEHOLD_SCOPE_URL` là URL gốc của API ứng dụng PF. Compose dev nối
`http://api-platform:8080`. URL không được chứa userinfo/query/fragment.
Không đặt URL thì chỉ tính năng định danh hộ native trả 503. Không gọi PF lúc
khởi động, tránh dependency vòng với Vault.

Endpoint PF có TLS/mTLS: cấu hình CA tin cậy bằng `AG_PLATFORM_CA`; nếu yêu cầu
chứng thư client, đặt đủ `AG_PLATFORM_CLIENT_CERT` và `AG_PLATFORM_CLIENT_KEY`.
Không tắt kiểm chứng TLS. Chuyển tiếp token người dùng gốc; cấu hình audience
phải cho phép token đó được cả hai API xác minh, không dùng tài khoản dịch vụ
để thay quyền người dùng.

## Phần còn lại trước khi hoàn tất AH-285

1. Luồng tạo/sửa định danh native với CCCD 12 số, SĐT 10 số bắt đầu bằng 0,
   chống trùng trong tenant và command/receipt bền vững khi PF hoặc Vault lỗi.
2. Mã hộ sinh tự động và bất biến; HTX bắt buộc, không đổi sau tạo. Diện tích
   canh tác > 0, chăn nuôi >= 0; giữ kiểm tra tổng diện tích vụ đang hoạt động.
3. Ngừng hoạt động và xóa theo lịch sử sản xuất/phiếu thu gom qua owner; không
   tự bỏ qua dependency nguồn và không làm mất lịch sử kiểm toán.
4. Form tạo/sửa/xóa và chi tiết trên web theo cùng ma trận server; Admin chọn
   HTX, Đại diện dùng HTX mình. Chỉ mở quyền ghi Admin cùng giao diện và test.
5. Danh sách/chi tiết chỉ đọc trên app, bản che dành cho Member; đầy đủ CI,
   tự review rồi merge trước khi chuyển story sang trạng thái hoàn tất.

Chưa thay luồng KN hiện có, chưa mở rộng quyền ghi Admin, chưa đánh dấu story
hoàn tất chỉ vì phần xác minh định danh đã chạy được.
