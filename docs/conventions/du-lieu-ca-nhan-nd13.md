# Dữ liệu cá nhân và vị trí theo NĐ 13/2023 (AH-9)

Trang này đối chiếu bốn tiêu chí của AH-9 (LEGAL-01, REQ-003, NFR-06, NFR-12) với cơ chế đang có trong mã, và ghi các
điểm còn chờ quyết định. Quy tắc chung: PII của nông hộ chỉ ở `vault/identity` (AGENTS.md, lỗi bắt buộc tránh #7).

## AC1 — Che số điện thoại

| Nơi hiển thị | Cơ chế |
|---|---|
| Danh sách nông hộ (web, app) | `masked_phone` từ kho định danh (`vault/identity/domain.MaskPhone`) |
| Danh sách tài khoản HTX | SĐT che ở máy chủ (AH-223, `accounts/handler/native_users.go`) |
| Hộ được HTX khác chia sẻ | Luôn che SĐT, CCCD (AH-286) |
| Liên hệ HTX cho người không quản lý | Che (`party/app/htx_contact.go`) |

SĐT đầy đủ chỉ hiện ở thao tác có chủ đích của người có quyền, mỗi lần đều ghi nhật ký truy cập (AC3): hồ sơ hộ
(`contact_access=full` cho Admin/Đại diện HTX), nút "Gọi", chi tiết tài khoản HTX, hồ sơ HTX (người đại diện) cho
Admin/Đại diện HTX, liên hệ cán bộ HTX của nông dân.

## AC2 — Mã hóa khi lưu

| Dữ liệu | Cơ chế |
|---|---|
| Họ tên, SĐT, CCCD, địa chỉ của nông hộ | Mã hóa phong bì AES-256-GCM theo từng hộ, khóa dữ liệu bọc bằng KEK; tìm theo SĐT qua blind index HMAC ([ADR-0004](../adr/0004-ma-hoa-phong-bi-theo-ho.md)) |
| Liên hệ HTX | Kho định danh như trên |
| Ảnh (kể cả EXIF) | Mã hóa phong bì ở `pf_files` ([envelope-format.md](../dev/envelope-format.md)) |
| Họ tên, SĐT tài khoản HTX | Chỉ ở Keycloak (thuộc tính người dùng), không lưu ở CSDL ứng dụng |
| Toạ độ GPS ghi nhận (vụ, nhật ký, ảnh, thửa) | **Chưa mã hóa**: cột PostGIS bản rõ, bảo vệ bằng RLS theo tenant/HTX và mã hóa đĩa — xem "Chờ quyết định" |

## AC3 — Nhật ký truy cập

| Lượt xem | Sổ |
|---|---|
| Mọi lần giải mã định danh nông hộ (hồ sơ, nút Gọi, nhãn danh sách) | `vault.access_log` |
| Liên hệ HTX | `vault.access_log` |
| SĐT đầy đủ của tài khoản HTX (chi tiết tài khoản; liên hệ cán bộ HTX của nông dân) | `pf_accounts.pii_view` |
| Mở tệp/ảnh | `pf_files.access_log` |

Ghi sổ lỗi thì không trả dữ liệu (đóng khi lỗi).

## AC4 — Không lộ trong log

- `archlint` (analyzer `agpiilog`, `tools/archlint/piilog.go`) chặn đưa trường có thẻ `pii:"true"` vào slog/log/fmt/errors.
  Định danh nông hộ (`cooperative/port.HouseholdIdentity`, `IdentityLabel`) và toạ độ GPS (`season/domain.GPS`, payload
  đồng bộ, DTO, request) mang thẻ `pii:"true"` (AH-9).
- Log truy cập HTTP chỉ ghi mẫu route, không ghi query string (`shared/httpx`).

## AH-293 — CCCD, SĐT (NFR#5, LEGAL-01)

- Lưu: CCCD, SĐT của nông hộ chỉ ở kho định danh (`vault.identity.phone_ct`, `national_id_ct`, AES-256-GCM theo khóa
  từng chủ thể); pf không có cột rõ (AC-N1).
- Tìm: `GET /v1/cooperatives/{ref}/households?phone=` hoặc `?national_id=` khớp ĐỦ số qua chỉ mục mù
  (`/v1/household-identities/phone-lookup`, `/national-id-lookup` của kho); một phần số → 422, không tìm gần đúng (AC-N2).
  HTX được chia sẻ không tra cứu được (403). Lưu ý: số tra cứu đi trong query string — cấu hình proxy/ingress không ghi
  query string vào log truy cập.
- Che ở máy chủ: Thành viên HTX và Đại diện HTX xem hộ của HTX được chia sẻ nhận SĐT 4 đầu/3 cuối, CCCD 3 đầu/3 cuối
  (`vault/identity/app.householdMask`); Admin, Đại diện HTX xem hộ HTX mình thấy đầy đủ ở hồ sơ hộ (AC-N3). Danh sách hộ
  luôn chỉ có SĐT đã che, không có CCCD.

## Chờ quyết định

1. **Toạ độ GPS khi lưu** (Q-32): mã hóa theo hộ (mất truy vấn không gian trực tiếp, cần cột phụ đã làm thô cho bản
   đồ/thống kê) hay chấp nhận RLS + mã hóa đĩa. Cần ADR do chủ dự án duyệt; cũng gỡ việc tạm giữ ghi ở
   [ah-51.md](../dev/ah-51.md).
2. **Nhật ký đọc toạ độ**: ghi nhật ký khi trả toạ độ gắn với hộ (chi tiết vụ, nhật ký việc, ranh giới thửa) — phụ
   thuộc quyết định 1.
3. **PII tài khoản HTX ở Keycloak**: chấp nhận mã hóa CSDL của Keycloak hay tách sang kho riêng.
4. **KEK môi trường thật**: dev dùng `AG_VAULT_KEK`; KMS/HSM cho môi trường thật cần chốt nguồn và quy trình xoay khóa.
