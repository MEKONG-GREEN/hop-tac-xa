# Vai trò và ma trận quyền App HTX (FN-05)

Nguồn: BRD Quản trị phân quyền App HTX v3.0 (FN-05, ADM-01, TECH-01, Mục V), quyết định của nhóm dự án ngày 16/11/2026 và 17/11/2026 (AH-272). App HTX có **03 vai cố định**. Quyền theo ma trận dưới đây, không chỉnh trên giao diện, không có quyền riêng cho từng tài khoản (FN-05/BR-02). Không có màn hình xem hay chỉnh vai.

## Vai trong BRD và vai trong mã

| Vai BRD | Vai realm trong mã | Ghi chú |
|---|---|---|
| Admin | `platform_admin` | Vai quản trị toàn hệ thống, đã có; vẫn bắt buộc yếu tố thứ hai theo chính sách hiện hành. Không kiêm vai khác. |
| Đại diện HTX | `coop_manager` | Đã có. Phạm vi là HTX gắn với tài khoản (`org_ref`). |
| Thành viên HTX | `htx_member` | Vai mới (AH-272). Người của HTX được cấp tài khoản để thao tác (KH-01); phạm vi là HTX của `org_ref`. Không dùng lại `farmer`: `farmer` là nông dân xem ruộng của hộ mình, mà theo KH-01 nông dân không có tài khoản. |

Lý do dùng lại hai vai đang có: không phải sửa phần kiểm quyền ở mọi API và mọi test đang dùng chúng. Đổi lại, tên vai trong mã không trùng tên vai trong BRD; bảng trên là nơi đối chiếu.

`htx_member` có ở realm Keycloak (`deploy/compose/keycloak/agrigreen-realm.json`), máy chủ (`cooperative/port`, `party/port`, `db.NodeBoundRoles`, danh mục vai của `pf/accounts`), `app/HTX/app.yaml` và web (`src/auth/principal.ts`). App di động thuộc AH-303.

## Ma trận Mục V (bản v3.0)

Ký hiệu: C = tạo, R = xem, U = sửa, D = xóa, L = khóa hoặc mở khóa, P = đặt lại mật khẩu, X = xuất file, - = không có quyền.

| Mã | Chức năng | Admin | Đại diện HTX | Thành viên HTX |
|---|---|---|---|---|
| FN-01 | Đăng nhập, đăng xuất | Có | Có | Có |
| FN-02 | Danh mục đơn vị hành chính | CRU (ngừng sử dụng thay cho xóa) | - | - |
| FN-03 | Quản lý HTX | CRUD (mọi HTX) | RU (xem HTX của mình và HTX được chia sẻ; sửa HTX của mình) | R (HTX của mình) |
| FN-04 | Quản lý hộ xã viên | CRUD (toàn hệ thống) | CRUD (HTX của mình); R (hộ của HTX được chia sẻ) | R (HTX của mình) |
| FN-07 | Quản lý tài khoản của Admin | CRUL (mọi tài khoản; tạo cả 03 vai trò) | - | - |
| FN-08 | Quản lý tài khoản của Đại diện HTX | - | CRUL (Thành viên HTX của HTX mình) | - |
| FN-09 | Liên kết và chia sẻ dữ liệu giữa HTX | - | CRUD (gửi, duyệt, từ chối, thu hồi yêu cầu của HTX mình) | - |
| FN-10 | Gửi mã OTP đặt lại mật khẩu cho người khác | P | P (Thành viên HTX của HTX mình) | - |
| FN-10 | Quên mật khẩu, tự đổi mật khẩu | U | U | U |
| FN-11 | Cấu hình hệ số rơm | CRU | - | - |
| FN-12 | Thống kê vụ của Thành viên HTX | R (mọi HTX) | R (HTX của mình và HTX được chia sẻ) | R (HTX của mình) |
| FN-13 | Thống kê vụ của Đại diện HTX | R (mọi HTX) | R (HTX của mình và HTX được chia sẻ) | - |
| FN-14 | Thống kê vụ của Admin | R | - | - |
| FN-15 | Bộ lọc và xuất báo cáo | RX | RX | R (chỉ chọn vụ, không xuất) |

Trong tệp dữ liệu, FN-01 ghi `R` (nghĩa là "được dùng"); hai dòng FN-10 có khóa `FN-10-OTP` và `FN-10-SELF`. Phạm vi dữ liệu trong ngoặc ("HTX của mình", "HTX được chia sẻ") chỉ ghi ở đây, không mã hoá vào tệp: phạm vi do từng API kiểm (AH-273, FN-09).

## Nguồn duy nhất và lệnh sinh lại

- Dữ liệu: [`contracts/htxroles/htx-roles.v1.json`](../../contracts/htxroles/htx-roles.v1.json). Máy chủ đọc qua gói Go `contracts/htxroles` (`go:embed`).
- Web: `apps/web-htx/src/api/generated/htx-roles.ts` sinh từ tệp trên bằng `scripts/gen-role-matrix.mjs`; chạy `npm run gen:api` (hoặc `npm run gen:modules`) trong `apps/web-htx`, kiểm bằng `npm run check:api`. Không sửa tay tệp sinh.
- Web dựng menu và nút từ ma trận qua `src/auth/permissions.ts`: mục menu gắn mã chức năng (`fn`) được cộng các vai ma trận cho xem (R); nút thao tác ẩn khi `matrixAllows` trả false. Không rải điều kiện theo ba vai trong từng màn.
- Ẩn menu, ẩn nút chỉ là giao diện. Máy chủ kiểm quyền ở mọi yêu cầu (FN-05/BR-03, TECH-01).

## Máy chủ kiểm quyền của Thành viên HTX

- **Lớp dịch vụ:** `htx_member` chỉ có trong danh sách vai của API đọc mà ma trận cho phép và đã giới hạn được theo HTX (FN-06, mục dưới): danh sách HTX (`GET /v1/cooperatives`), danh sách tổ chức (`GET /v1/orgs`), xem một HTX (`GET /v1/cooperatives/{ref}`) và danh sách nông hộ của một HTX (`GET /v1/cooperatives/{ref}/households`), tất cả chỉ HTX của `org_ref`. Test đối chiếu (`roles_matrix_test.go` của `cooperative/member/app` và `party/app`) khẳng định phần quyền này khớp ma trận.
- **Lớp lắp ráp:** `cmd/api-platform/assembly/memberguard.go` chặn mọi request ghi (POST/PUT/PATCH/DELETE) của tài khoản chỉ là Thành viên HTX trên Application API và Admin API (403 `forbidden`), trừ route trong `member_write_routes` của ma trận. Test dạng bảng `cmd/api-platform/htx_member_routes_test.go` duyệt mọi route ghi.
- **Thao tác cá nhân không phải ghi dữ liệu nghiệp vụ** (không thuộc chức năng nào của Mục V, nên BR-03 không cấm): Thành viên HTX được đánh dấu đã đọc thông báo của mình và đăng ký push token cho thiết bị của mình (pf/notify). Ba route có trong `member_write_routes`: `POST /v1/notifications/read-all`, `POST /v1/notifications/:id/read`, `POST /v1/devices/:id/push-token`. Dịch vụ vẫn tự giới hạn: chỉ thông báo người gọi thấy được (`viewer`), chỉ thiết bị đã đăng ký cho chính tài khoản. Test `htx_member_routes_test.go` khẳng định route có trên router thật và Thành viên HTX gọi tới được dịch vụ.
- **Ticket sau mở quyền ghi cho Thành viên HTX** (mở vụ, ghi nhật ký theo KH-01): thêm vai vào dịch vụ **và** thêm route dạng `"POST /v1/…"` (mẫu đường dẫn của gin) vào `member_write_routes`, cập nhật ma trận nếu BRD đổi. Thiếu một trong hai thì request vẫn bị chặn.
- API thống kê vụ (FN-12, FN-13) chưa có trên App HTX; khi làm, mở cho Thành viên HTX sau khi đã giới hạn theo HTX.

## Phạm vi dữ liệu theo HTX (FN-06)

Nguồn: FN-06, ADM-02, TECH-01; quyết định của nhóm dự án 17/11/2026 (AH-273).

- **Quy tắc dùng chung:** `htxroles.OwnCoopOnly(has, wider...)` trả true khi tài khoản chỉ được thấy HTX gắn với tài khoản (`org_ref`): có Đại diện HTX hoặc Thành viên HTX mà không có Admin và không có vai nào trong `wider` — các vai có phạm vi rộng hơn mà API đó nhận (vd `registry_admin`, `kn_officer`, `kn_province_admin`, `kn_national_admin`, `cgh_viewer`). Tài khoản kèm một vai rộng hơn (kể cả Thành viên HTX + khuyến nông) theo phạm vi của vai đó, khớp `lacksCoop` của web. Đang dùng ở `party/app.ownCoopOnly` (danh sách HTX, danh sách tổ chức, chi tiết HTX) và `cooperative/member/app.isOwnCoopOnly` (nông hộ).
- **Admin** (`platform_admin`) thấy mọi HTX. Các vai khuyến nông giữ phạm vi theo xã/tỉnh (`scopeFor`, không đổi).
- **API mới của App HTX** phải gọi quy tắc này (hoặc lọc theo `org_ref`): phạm vi theo xã (`scopeFor`, `authz.Scope`) không tự thu hẹp về một HTX.
- **Bản ghi ngoài phạm vi** trả 404 (`party.org_not_found`), không lộ việc bản ghi tồn tại (FN-06/EX-01). Danh sách chỉ có HTX của mình.
- **Tài khoản chưa gắn HTX** (FN-06/EX-02): máy chủ giữ nguyên — danh sách rỗng, API cần Nút trả 403 `node_scope_required` ("tài khoản chưa gắn đơn vị"). Web nhận ra từ thông tin đăng nhập (`lacksCoop` ở `src/auth/permissions.ts`) và ở khung chung (`AppShell`) hiện "Tài khoản chưa được gắn HTX. Liên hệ người cấp tài khoản." thay cho mọi màn `/htx/*`, không gọi API; màn khác nhận `node_scope_required` thì `ProblemAlert` hiện cùng câu.
- **Rời trang ngoài phạm vi** (web, `src/shell/notice.tsx`): người dùng App HTX mở đường dẫn chi tiết không được xem, hoặc màn chi tiết nhận 404 (`useExitOnNotFound`), được chuyển về trang đầu tiên được xem kèm câu "Không tìm thấy dữ liệu, hoặc bạn không có quyền xem dữ liệu này." — một câu cho cả hai trường hợp. Trang danh sách không thuộc quyền vẫn theo FN-05/EX-01 (chuyển, không kèm câu).
- **Ngoài phạm vi quy tắc này:** xem dữ liệu HTX khác khi được chia sẻ (FN-09); phạm vi hẹp hơn theo nông hộ của BRD Kế hoạch mùa vụ (AH-39).

## Kiêm hai vai

Một tài khoản có thể kiêm Đại diện HTX và Thành viên HTX (FN-05/BR-02); Admin không kiêm vai khác. Việc chọn vai làm việc khi đăng nhập thuộc AH-270. Tới khi có, tài khoản có cả hai vai dùng quyền của Đại diện HTX (`htxroles.IsMemberOnly`; trên web là hợp quyền của các vai ma trận).

## Mục thay thế tạm cho FN-02 và FN-07

Web chưa có màn "Tài khoản" (FN-07) và "Đơn vị hành chính" (FN-02) của App HTX. Theo quyết định 17/11/2026, quy tắc ẩn/hiện áp trên hai mục đang giữ vai trò tương ứng: "Tài khoản cán bộ" (`/quan-tri/tai-khoan`, gắn `FN-07`) và "Danh mục dùng chung" (`/quan-tri/danh-muc`, gắn `FN-02`). Khi làm FN-02 và FN-07, các ticket đó đăng ký mục menu của mình với mã chức năng tương ứng.

## Trang đầu tiên được xem

Là mục đầu tiên theo thứ tự menu mà vai đang đăng nhập được xem (`homeFor` của web). Thành viên HTX mở đường dẫn không thuộc quyền (FN-05/AC-03, EX-01) thì được chuyển về trang đó. Vai không xem được mục nào thì về `/khong-co-quyen` ("Tài khoản không có quyền dùng chức năng nào"), không lặp chuyển hướng.
