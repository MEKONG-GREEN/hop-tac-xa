# ADR-0064: Bù FN-TK-01, FN-TK-02 trên nền FN-01, FN-07 đã làm

**Trạng thái:** Chấp nhận. **Ngày:** 23/11/2026 (AH-222, AH-223; epic AH-216). **Nguồn:** chủ repo chọn phương án "Bù phần
thiếu" ngày 23/11/2026, khi hai BRD còn khác nhau và PO chưa trả lời các câu hỏi trên AH-222, AH-223.

**Liên quan:** [ADR-0060](0060-man-dang-nhap-rieng-app-htx.md), [ADR-0062](0062-realm-htx-fn07.md),
[ADR-0063](0063-kenh-sms-otp-dat-lai-mat-khau.md), [vai trò App HTX](../conventions/vai-tro-app-htx.md).

## Bối cảnh

AH-222 (FN-TK-01) và AH-223 (FN-TK-02) theo BRD Thời tiết & Thu gom phụ phẩm trùng phạm vi với AH-268, AH-270, AH-275,
AH-276 theo BRD Quản trị phân quyền, đã làm và đưa vào `dev-agent`. Hai BRD khác nhau ở những điểm PO chưa chốt: số HTX
và số vai của một tài khoản, đăng nhập bằng tên đăng nhập, thêm/ngừng từng vai, Admin cấp lại mật khẩu tạm, độ dài mật
khẩu. Làm đúng BRD Thời tiết & Thu gom phụ phẩm sẽ đổi hành vi đã có.

## Quyết định

Giữ nguyên hành vi của FN-01, FN-07, FN-08, FN-10. Chỉ làm các AC, BR, EX của FN-TK-01, FN-TK-02 còn thiếu mà không trái
BRD Quản trị phân quyền:

| Mục | Cách làm |
|---|---|
| FN-TK-01/EX-02 | Đăng nhập realm HTX đúng mật khẩu mà token không còn vai App HTX → 403 `accounts.no_active_role` kèm câu BRD, không phát token, thu hồi phiên vừa phát. Guard vai làm việc của API và Vault trả cùng mã. |
| FN-TK-01/EX-03, AC-06 | `X-HTX-Role` là Đại diện HTX/Thành viên HTX không còn trong token đã làm mới → 409 `accounts.working_role_revoked`. Web và app đọc lại vai từ token: còn một vai thì dùng luôn (AC-01), còn hai vai thì về màn Chọn vai trò, kèm câu báo. App giữ nguyên dữ liệu chờ gửi. Thời điểm vai mất hiệu lực vẫn theo QTPQ-Q-23 (token được làm mới), không thu hồi phiên ngay. |
| UX-TP-09 (đánh dấu vai dùng lần trước) | Thẻ vai ghi "Dùng lần trước". Web lưu mã băm FNV của định danh phiên kèm tên vai ở localStorage, không lưu mã người dùng; app dùng mốc `htx.working_role` sẵn có. Không tự chọn vai thay người dùng. |
| FN-TK-02/AC-04, BR-03 (hạn 72 giờ) | Mật khẩu tạm đã cấp quá 72 giờ (createdDate của thông tin xác thực "password" ở Keycloak, `port.PasswordAgeReader`) → 403 `accounts.temporary_password_expired`. Chỉ kiểm sau khi mật khẩu tạm đúng; không đọc được mốc thì coi như hết hạn. Lấy lại bằng Quên mật khẩu (FN-10) hoặc đặt lại (AH-281). |
| FN-TK-02/EX-04 | Khóa Đại diện HTX đang hoạt động cuối cùng của HTX, hoặc chuyển tài khoản đó sang HTX khác → 409 `accounts.last_representative` "HTX sẽ không còn người duyệt kết quả lô"; gửi lại với `confirm_no_representative=true`. Là bước hỏi lại, không phải bất biến: hai Admin cùng lúc vẫn có thể để HTX trống. |
| FN-TK-02/BR-04 | Danh sách tài khoản (Admin và Đại diện HTX) trả SĐT đã che `09•• ••• 678`; tìm SĐT bằng query `phone` ở máy chủ (giữ FN-07/AC-02). Chi tiết trả SĐT đầy đủ sau khi ghi sổ chỉ ghi thêm `pf_accounts.pii_view` (người xem, tài khoản, trường, thời điểm; không chứa SĐT); ghi lỗi thì không trả. |

Các mục đã có từ trước, không làm lại: AC-01, AC-02, AC-03, AC-05, EX-01, BR-02, BR-03 của FN-TK-01 (AH-268, AH-270,
ADR-0060); AC-01, AC-02, AC-03, AC-05, AC-06, AC-07, BR-02, BR-05, EX-01, EX-03, EX-05 của FN-TK-02 (AH-275, AH-276,
AH-301).

## Chưa làm — chờ PO

- **Trái BRD Quản trị phân quyền:** một tài khoản nhiều vai ở nhiều HTX (FN-TK-01/BR-01; FN-07/BR-02 gắn đúng 01 HTX);
  thêm, ngừng, kích hoạt lại từng vai (FN-TK-02; FN-07/BR-03 không đổi vai sau khi tạo); không sửa SĐT, đổi số thì tạo tài
  khoản mới (FN-TK-02/BR-01; FN-07 cho sửa SĐT); mật khẩu người dùng tối thiểu 8 ký tự có chữ và số (FN-TK-02/BR-03; FN-01
  tối thiểu 06 ký tự); chỉ Admin quản lý tài khoản (FN-TK-02; FN-08 cho Đại diện HTX quản lý Thành viên HTX).
- **Phụ thuộc ticket khác:** EX-04 của FN-TK-01 — dữ liệu offline của vai khác vẫn đổi được vai (nền tảng offline AH-245;
  app hiện chặn đổi vai khi còn dữ liệu chờ gửi tạo dưới vai khác); BR-05 — dấu đếm thông báo theo vai trên thẻ (AH-243);
  "số việc cần xử lý" trên thẻ vai (UX-TP-09; BRD chưa định nghĩa); menu UX-TP-08 của các phân hệ Thời tiết, Thu gom phụ
  phẩm (các story sau của BRD đó); AC-04, BR-04 không tự duyệt phiếu cân (S16).

## Đánh đổi

- Danh sách che SĐT nên màn chi tiết phải gọi thêm một API (ghi một dòng sổ mỗi lần mở). Đổi lại SĐT đầy đủ chỉ lộ ở nơi có
  nhật ký.
- Phản hồi sau khi sửa/khóa vẫn có SĐT đầy đủ của tài khoản vừa thao tác (người thao tác đã nhập hoặc đã xem chi tiết).
