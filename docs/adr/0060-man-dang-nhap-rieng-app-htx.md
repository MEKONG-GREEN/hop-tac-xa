# ADR-0060: Màn đăng nhập riêng của App HTX

**Trạng thái:** Chấp nhận; yếu tố thứ hai của Admin (`platform_admin`) thay bằng mã OTP gửi qua email theo [ADR-0061](0061-admin-dang-nhap-otp-email.md) (17/11/2026). **Ngày:** 16/11/2026 (AH-268, FN-01). **Nguồn:** quyết định kiến trúc đăng nhập của người phụ trách dự án ngày 16/11/2026, phương án nhóm dự án 15/11/2026 và trả lời hai lượt của nhóm dự án cho các câu hỏi của dev-agent.

**Liên quan:** [ADR-0057](0057-tu-dat-lai-mat-khau-qua-email.md) (đặt lại mật khẩu), [ADR-0059](0059-tach-he-thong-hop-tac-xa.md) (App HTX), [runbook đăng nhập App HTX](../runbooks/dang-nhap-app-htx.md), [runbook MFA D24](../runbooks/keycloak-mfa-d24.md).

## Bối cảnh

App HTX đăng nhập qua trang của Keycloak (OIDC, PKCE); web và app không chạm vào mật khẩu. Cách đó không làm được ba yêu cầu của FN-01: hiện số lần thử còn lại (EX-02, AC-08), thông báo riêng khi bị khóa tạm 15 phút, và đăng nhập bằng tên đăng nhập hoặc SĐT của cùng một tài khoản (AC-09).

## Quyết định

App HTX có màn đăng nhập riêng trên web. Người dùng nhập tên đăng nhập hoặc SĐT và mật khẩu; web gửi về máy chủ (`POST /v1/public/auth/login`, `/refresh`, `/logout` ở `pf/accounts`); máy chủ kiểm mật khẩu **qua Keycloak** (direct grant) rồi trả token của Keycloak.

- **Keycloak giữ mật khẩu và phát token.** Máy chủ không lưu, không băm mật khẩu; phần kiểm token ở `internal/shared/authz` không đổi. Bí mật client lấy từ cấu hình triển khai; thiếu thì route đăng nhập trả 503 (fail-closed).
- **Một client, đăng nhập một bước.** Chỉ có `agrigreen-htx-login` (flow direct grant của realm, có nhánh OTP). Mỗi lượt đăng nhập gọi Keycloak đúng một lần, gửi mật khẩu kèm mã OTP nếu người dùng nhập; web có sẵn ô "Mã OTP" không bắt buộc. Tài khoản có OTP mà không gửi mã bị Keycloak từ chối như sai mật khẩu (`401 invalid_grant` cho cả hai), máy chủ trả đúng phản hồi sai tài khoản hoặc mật khẩu và lượt đó là một lần sai — không lộ tài khoản nào có OTP.
- **Yếu tố thứ hai không bị nới.** Keycloak là bộ đếm duy nhất, kể cả cho lần sai OTP; bộ đếm chỉ về 0 khi đăng nhập trọn vẹn thành công. Tài khoản có vai bắt TOTP (`port.LoginTOTPRoles`) mà chưa đăng ký: không bao giờ phát token, trả `accounts.login_setup_required`. Sau khi Keycloak phát token, máy chủ kiểm lại claim: vai bắt TOTP mà `amr` không có `otp` thì thu hồi phiên và không trả token.
- **Vì sao bỏ thiết kế hai client (ngày 16/11/2026).** Bản đầu dùng thêm client `agrigreen-htx-login-check` chỉ kiểm mật khẩu để biết khi nào hỏi OTP ở bước hai. Mỗi lần client đó kiểm mật khẩu đúng, Keycloak coi là đăng nhập thành công và xóa bộ đếm lần sai. Trang đăng nhập Keycloak vẫn còn dùng, nên người đã biết mật khẩu có thể đoán OTP ở trang đó rồi gọi API mới chỉ với mật khẩu để xóa bộ đếm, lặp lại không giới hạn. Bộ đếm riêng ở máy chủ không thấy các lượt đoán ở trang Keycloak nên không đóng được lỗ này; chỉ bỏ hẳn đường kiểm mật khẩu không kèm yếu tố thứ hai mới đạt ràng buộc "không nới xác thực hai yếu tố". Client, flow, cấu hình và biến môi trường của đường đó đã gỡ.
- **Một bộ đếm cho tài khoản có thật: của Keycloak** (5 lần sai khóa 15 phút; hai lần sai cách nhau dưới 1 giây khóa 60 giây; cửa sổ 24 giờ). Khóa của Keycloak bật theo cả realm nên không thể tắt riêng cho một nhóm tài khoản, và hai bộ đếm song song sẽ lệch khi có lần sai đi qua trang đăng nhập cũ hoặc khi Admin mở khóa. Máy chủ đọc `numFailures`/`lastFailure` **trước** khi kiểm mật khẩu rồi cộng lần vừa sai (Keycloak có thể ghi nhận lần sai chậm một nhịp), không đọc lại sau lần sai. Nút "Mở khóa" của Admin giữ nguyên và mở cho cả màn mới.
- **Mã định danh không tồn tại** (và SĐT khớp từ hai tài khoản) có bộ đếm riêng ở `pf_accounts.login_probe`: cùng ngưỡng, cùng thông báo, cùng cách khóa để không dò được tài khoản nào có thật. Khóa bảng là HMAC-SHA256 có khóa bí mật (`AG_LOGIN_ID_KEY`) của mã đã chuẩn hóa — không lưu mã ở dạng đọc được (tên đăng nhập và SĐT là dữ liệu cá nhân). Bảng `ag:no-rls` (mã chưa gắn tenant), vai ứng dụng không có quyền bảng, chỉ gọi hai hàm `SECURITY DEFINER` theo mẫu `shared.chain_head`.
- **Tài khoản "Đã khóa" (FN-07)** báo EX-03 dù mật khẩu đúng hay sai (Keycloak không cho kiểm mật khẩu của tài khoản đã khóa). Đánh đổi đã chấp nhận: người ngoài biết một mã định danh thuộc tài khoản đã khóa; tài khoản đang hoạt động và mã không tồn tại vẫn giống hệt nhau, và API có giới hạn tần suất theo IP.
- **Tra tài khoản:** khớp đúng tên đăng nhập thì lấy (duy nhất); không có mới tra theo SĐT; SĐT khớp từ hai tài khoản trở lên coi như sai và ghi một dòng cảnh báo không kèm SĐT. SĐT duy nhất toàn hệ thống (BR-07) thuộc FN-07/FN-08, không sửa ở đây.
- **Token chỉ nằm trong bộ nhớ của tab** (kể cả refresh token), giữ quy tắc của `tokenStore.ts`. Làm mới và đăng xuất đi qua máy chủ; đăng xuất thu hồi phiên ở Keycloak.
- **Không đòi `Idempotency-Key`** cho API đăng nhập; thân chứa mật khẩu không vào kho idempotency, outbox, log, đo đếm hay thông báo lỗi.
- **Admin đầu tiên** tạo bằng lệnh `cmd/bootstrap-admin` (không có màn hình): vai `platform_admin`, tenant và đơn vị là cờ bắt buộc, mật khẩu tạm in ra stdout đúng một lần, tài khoản phải đổi mật khẩu ở lần đăng nhập đầu. Từ ADR-0061: email bắt buộc (đã xác minh), không đăng ký TOTP; Admin đăng nhập hai bước bằng mật khẩu và mã OTP gửi qua email.

## Nhóm tài khoản theo luồng

- Mọi tài khoản đăng nhập **web App HTX** (`apps/web-htx`, không ở chế độ giả lập) dùng màn mới.
- Tài khoản còn việc lần đầu (`UPDATE_PASSWORD`, `CONFIGURE_TOTP`, kể cả Admin vừa khởi tạo) nhận `accounts.login_setup_required`; nút trên màn chuyển sang trang Keycloak cũ để hoàn tất, cho tới khi AH-269 làm màn đổi mật khẩu lần đầu.
- **App Flutter** (`agrigreen-htx-app`) và mọi client OIDC khác vẫn đăng nhập qua trang Keycloak. Luồng OIDC/PKCE cũ (`oidc.ts`, `/login/callback`, `/login/silent`) không bị gỡ trong ADR này.

## Đánh đổi

- **Mật khẩu đi qua mã của web và máy chủ**, khác nguyên tắc trước đây là chỉ Keycloak chạm vào mật khẩu. Đổi lại FN-01 đạt từng tiêu chí và từng câu thông báo; các ràng buộc trên giữ rủi ro trong giới hạn.
- **Tải lại trang hoặc mở tab mới thì phải đăng nhập lại** với phiên của màn mới, vì không có cookie SSO của Keycloak. Giữ phiên bằng cookie httpOnly cần đổi CORS sang chế độ có credentials và chống CSRF — để việc sau.
- **Thêm một client vào realm:** môi trường đã nhập realm phải cập nhật theo runbook.
- **Người dùng có OTP phải nhập mã ngay ở lần đầu.** Không có bước hỏi OTP riêng; ô "Mã OTP" có ghi chú, và thông báo sai nhắc nhập cả mã OTP. Quên nhập mã tốn một lần thử trong 5 lần.
- **Thời gian phản hồi không cân bằng:** mã không tồn tại không gọi Keycloak nên nhanh hơn; thông báo, cách đếm và cách khóa giống hệt. Không cân thời gian; giới hạn tần suất theo IP làm giảm khả năng dò theo thời gian.
- **Bộ đếm cho mã không tồn tại ở DB** (không ở bộ nhớ tiến trình) để nhiều bản sao và khởi động lại vẫn cho kết quả giống tài khoản thật; đổi lại cần một bảng `ag:no-rls` truy cập qua hàm `SECURITY DEFINER`.

## Ngoài phạm vi

Chặn tài khoản bị khóa ở thao tác kế tiếp trên mọi API (EX-06, AC-N2); màn đăng nhập trên app Flutter; màn đổi mật khẩu lần đầu (AH-269); chọn vai trò làm việc (AH-270, AH-222); menu, nút và phạm vi dữ liệu theo vai (AH-272, AH-273); đăng nhập bằng email ở màn mới; cân bằng thời gian phản hồi; gỡ luồng OIDC cũ.
