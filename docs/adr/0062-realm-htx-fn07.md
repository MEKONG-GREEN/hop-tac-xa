# ADR-0062: Realm HTX riêng cho FN-07

**Trạng thái:** Chấp nhận. **Ngày:** 20/11/2026. **Nguồn:** người dùng chốt FN-07 và chọn tách realm HTX trong AH-275.

FN-07 dùng một HTX cho mỗi tài khoản, một vai hoặc hai vai Đại diện/Thành viên cùng HTX; Admin không gắn HTX. Tên đăng nhập và vai bất biến sau tạo, ngoại trừ luồng bổ sung Thành viên FN-08. Đặt lại mật khẩu qua OTP, không có thao tác Admin đặt lại mật khẩu người khác. Danh mục dùng chung cho Đại diện/Thành viên có danh sách và xuất Excel chỉ đọc (quyết định VTKM-Q-07 cho AH-274).

FN-07 yêu cầu mật khẩu tạm ngẫu nhiên 8 ký tự chữ/số, hiển thị một lần và bắt đổi lần đầu. Realm `agrigreen` đang phục vụ KN với mật khẩu tối thiểu 12 ký tự (ADR-0041). Người dùng chọn **tách realm**, thay vì tăng mật khẩu tạm lên 12 hoặc hạ chính sách KN.

## Quyết định

- Realm mới `agrigreen-htx`, chính sách `length(8) and notUsername and passwordHistory(5)`. Không đổi realm `agrigreen` hoặc `agrigreen-kn-admin`.
- Vai nghiệp vụ: `platform_admin`, `coop_manager`, `htx_member`. Vai kỹ thuật `login_totp_required` chỉ giữ hợp đồng của các flow kiểm OTP hiện có, không được gán qua FN-07. Không cấp vai KN, Vault hoặc dịch vụ hệ khác.
- Các client riêng: `agrigreen-htx-api` (audience), `agrigreen-htx-accounts` (Admin REST), `agrigreen-htx-web` (PKCE), `agrigreen-htx-login` (direct grant máy chủ), `agrigreen-htx-app` (PKCE app). Hai client bí mật dùng biến `AG_HTX_KC_CLIENT_SECRET`, `AG_HTX_KC_LOGIN_CLIENT_SECRET`, khác bí mật KN.
- Một bản triển khai API chỉ tin **một issuer**. API, Vault, web, app và worker HTX dùng cùng realm. Không thêm cơ chế chọn issuer theo token chưa xác thực hoặc thử lại token qua realm khác. API từ chối cấu hình trộn issuer/audience/client HTX và KN trước khi mở DB.
- Tenant và HTX tiếp tục lấy từ claim do IAM quản trị. Tách realm không chuyển tenant lưu ký, dữ liệu nghiệp vụ hoặc tạo HTX mới (ADR-0059). Không chuyển tự động người dùng KN sang HTX; mã người dùng mới phải được liên kết qua luồng quản trị có kiểm soát.
- Admin tiếp tục đăng nhập qua mật khẩu + OTP email ADR-0061; không thêm đường bỏ OTP. Thiếu SMTP tiếp tục từ chối đăng nhập.

## Triển khai

`deploy/compose/keycloak-htx/agrigreen-htx-realm.json` là cấu hình realm riêng. Tệp không nằm trong thư mục import mặc định của KN. `deploy/compose/docker-compose.htx.yml` chọn realm cho bản triển khai HTX:

```powershell
docker compose -f deploy/compose/docker-compose.yml -f deploy/compose/docker-compose.htx.yml config --quiet
```

Các bí mật đặt trong tệp môi trường được gitignore, không dán vào ticket. Chạy HTX trong project/môi trường riêng; cấu hình cổng/hostname cho môi trường đó trước khi chạy đồng thời với KN. Web phải build lại vì issuer được nhúng lúc build. App truyền issuer/client HTX tương ứng qua cấu hình triển khai hiện có.

Import JSON chỉ tạo realm mới, không cập nhật realm đã tồn tại. Môi trường thật cần kiểm tra chính sách/clients và discovery trước khi chuyển lưu lượng. Gói này không triển khai hoặc thay realm đang chạy.

## Luồng FN-07 và chọn vai

Admin khởi tạo dùng `bootstrap-admin -htx -tenant <UUID> -username <tên> -email <email> -phone <SĐT> -first-name <tên> -last-name <họ>`, không truyền `-org`. Luồng cũ vẫn yêu cầu đơn vị. Tài khoản tiếp theo tạo tại `/quan-tri/tai-khoan-htx`, API Admin `/v1/accounts/htx-users`. Native realm chỉ dùng catalog ba vai; các đường tạo/import, gán vai/đơn vị, sửa hồ sơ KN và gửi liên kết đổi mật khẩu của luồng cũ không dùng để thay quy tắc FN-07.

`AG_HTX_ACCOUNTS_OPERATION_KEY` là khóa HMAC 32 byte, mã hóa base64, để ràng buộc thao tác tạo với nội dung và người gọi. Giữ ổn định qua restart/retry; thay khóa cần chuyển trạng thái các thao tác theo kế hoạch. Không ghi mật khẩu, hồ sơ IAM hoặc chỉ mục tìm SĐT vào PF. Cache phát lại chỉ có tham chiếu và trạng thái, các trường họ tên/tên đăng nhập/email/SĐT được bỏ giá trị; giao diện giữ tên của yêu cầu để thông báo retry.

PF giữ một thao tác cấp tài khoản IAM ở trạng thái `pending` trên toàn realm tại một thời điểm, xuyên tenant lưu ký. Kiểm tra username/SĐT/email duy nhất thực hiện tại IAM trong khoảng này, ngoài transaction DB. Lượt khác nhận 503 để thử lại. Khi IAM đã được bù xóa chắc chắn, cùng khóa có thể thử lại. Khi kết quả tạo hoặc COMMIT chưa rõ, thao tác giữ `pending`; không tự hết hạn để tránh cấp trùng. Vận hành phải đối chiếu IAM với dòng sổ theo mã thao tác trước khi hoàn tất hoặc bù, không xóa claim chỉ vì đã lâu. Nếu ghi sổ lỗi sau khi có user, backend yêu cầu IAM vô hiệu hóa user; không xóa user khi COMMIT có thể đã thành công. Đây là giới hạn thông lượng có chủ đích của MVP.

IAM giữ thuộc tính `htx_creation_ref` bằng UUID thao tác, chỉ quản trị xem/sửa; thuộc tính không được ánh xạ vào token hoặc phản hồi hồ sơ. Tìm user theo thuộc tính này khi phản hồi tạo bị mất. Reservation HTX con dùng khóa `SHA256(actor_ref + ":FN-07:" + creation_ref)` (hex), nên vận hành đối chiếu được cả IAM, dòng sổ và reservation mà không cần giữ hồ sơ hay khóa HTTP gốc trong PF. HTTP 4xx IAM đã từ chối chắc chắn được bù/release; lỗi mạng hoặc 5xx chưa rõ kết quả tiếp tục giữ slot. Quyền `view-realm` của service account chỉ phục vụ đọc danh mục vai để cấp vai; không cấp `realm-admin` hoặc `manage-realm`.

Chọn vai là phần kỹ thuật cần cho AC tài khoản hai vai của AH-275 và làm nền AH-270. Web và app giữ lựa chọn trong phiên, gửi `X-HTX-Role` ở từng request. Máy chủ kiểm vai thuộc token đã xác thực rồi đặt trần phân quyền bằng phép giao; không sửa role claim, actor, tenant, HTX hoặc MFA. Tài khoản hai vai chưa chọn bị chặn dữ liệu nghiệp vụ (409 `accounts.working_role_required`); chỉ được đọc thông tin khởi tạo và đồng ý dữ liệu. `/v1/me` trả vai có hiệu lực sau chọn. Token một vai tự dùng vai đó.

Vault kiểm lại lựa chọn theo JWT gốc và chuyển tiếp đúng vai khi hỏi bằng chứng phạm vi từ chủ dữ liệu. Cache idempotency gắn với vai làm việc để kết quả của Đại diện không phát lại dưới vai Thành viên. Vai trong UI được thu hẹp tương ứng. Đổi vai có xác nhận trước khi bỏ form; app giữ checkpoint vai và danh tính của dữ liệu chờ để không gửi các khóa bất biến dưới vai khác. Các phần quan hệ chia sẻ của AH-270 cần AH-274/AH-278, không coi toàn bộ AH-270 hoàn tất chỉ vì có bộ chọn vai.
