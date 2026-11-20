# AH-285 — hộ xã viên theo luồng HTX hiện tại

Nhánh đích: `dev-agent`. AH-51 đã hoàn tất; AH-283 là dependency hồ sơ HTX.
Requirement đối chiếu comment Jira ngày 17/11/2026 và ma trận FN-04 dùng chung.

## Luồng dữ liệu

FN-04 dùng cùng bảng hộ/membership hiện hữu và ma trận `contracts/htxroles`:

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
- GET `/v1/households/:ref/profile` nối cổng đọc hẹp trên Application API
  hiện hữu. Dialog chi tiết chỉ tải lúc mở, không lưu trình duyệt; đổi danh
  tính/phạm vi hoặc đóng dialog sẽ dọn dữ liệu. Test HTTP đi qua cả PF → Vault
  → owner và test UI giữ bản che, dọn kết quả cũ khi request chưa hoàn tất.

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

## Ghi và xóa hồ sơ

1. Luồng tạo/sửa định danh native nhận CCCD 12 số, SĐT 10 số bắt đầu bằng 0,
   chống trùng trong tenant và command/receipt bền vững khi PF hoặc Vault lỗi.
2. Mã hộ sinh tự động và bất biến; HTX bắt buộc, không đổi sau tạo. Diện tích
   canh tác > 0, chăn nuôi >= 0; giữ kiểm tra tổng diện tích vụ đang hoạt động.
3. Ngừng hoạt động và xóa theo lịch sử sản xuất/phiếu thu gom qua owner; không
   tự bỏ qua dependency nguồn và không làm mất lịch sử kiểm toán.
4. Form tạo/sửa/xóa trên web theo cùng ma trận server; Admin chọn
   HTX bắt buộc khi tạo, Đại diện dùng HTX mình. Màn sửa giữ nguyên HTX/mã hộ.
5. Danh sách/chi tiết chỉ đọc trên app, bản che dành cho Member. Chi tiết chỉ
   đọc online trong bộ nhớ, không ghi LocalDb hoặc hàng đợi đồng bộ.

POST `/v1/household-profiles`, POST `/v1/households/:ref/profile/revisions`
và POST `/v1/households/:ref/profile/deletions` nhận Idempotency-Key. Command
PF chỉ giữ actor, mã tham chiếu và metadata; receipt Vault giữ HMAC có khóa
của payload, không ghi CCCD/SĐT rõ hoặc digest dễ dò vào PF. Receipt và cập
nhật bản mã/blind-index/audit của Vault commit trong cùng transaction. Mỗi
hộ chỉ có một command đang chờ; retry cũ so payload rồi trả receipt, không
ghi lại định danh mới. Biết chắc CCCD/SĐT trùng thì giải phóng command lỗi;
kết quả chưa rõ giữ chốt. Chưa có worker đối soát/thử lại tự động: web giữ
khóa trong RAM, lỗi chưa rõ yêu cầu thử lại cùng thao tác.

Trong lúc command chờ, đọc chi tiết trả 503; mở vụ mới và các luồng ghi KN
khác bị chặn tại DB. Vụ đã mở tiếp tục làm việc. Guard diện tích khóa cùng
dòng hộ với bên mở vụ, kiểm lại tổng diện tích đang canh tác khi commit.

Xóa là xóa logic khỏi các danh sách/chi tiết, giữ metadata, audit và định
danh mã hóa/chỉ mục chống trùng tại Vault. Mã hộ sinh từ UUID, bất biến.
CCCD đã lưu vẫn thuộc không gian chống trùng sau khi xóa logic. Không xóa
cứng sổ hoặc tự giải phóng PII dùng chung với luồng KN. Mỗi lần xóa kiểm
port lịch sử mùa vụ, gồm cả vụ đã đóng/hủy. DB kiểm lại dưới khóa dòng hộ
và yêu cầu source command/audit event; producer không ghi mới vào hộ đã xóa.
Lịch sử trước một lần chuyển HTX theo KN được kiểm toàn tenant trong hàm
owner chỉ trả boolean, phục hồi nguyên phạm vi node/xã sau khi kiểm.

Phiếu thu gom theo hộ chưa có producer (AH-232..234, theo quyết định 15/11).
Khi triển khai phải nối owner đó vào port lịch sử và protocol khóa hộ trước
khi bật ghi phiếu; không dùng nguồn thống kê/tổng HTX để thay lịch sử hộ.
Luồng KN hiện hữu được giữ; cập nhật metadata của KN tăng revision và tôn
trọng chốt native. Không gán thêm vai Vault rộng cho Admin/Đại diện.

Chỉ chuyển story hoàn tất sau tự review, đầy đủ CI đúng SHA và merge vào
`dev-agent`. Phần hộ của HTX được chia sẻ thuộc AH-286/FN-09.
