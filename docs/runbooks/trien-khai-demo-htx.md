# Triển khai máy demo App HTX — `https://demo-htx.mekonggreen.vn`

Kịch bản dựng một máy demo App HTX (web + backend + Keycloak) trên **một VPS, Docker Compose, một tên miền**. Tệp triển khai: [`deploy/demo/`](../../deploy/demo/). Chỉ dữ liệu demo/tổng hợp; không nạp dữ liệu cá nhân thật.

> **Trạng thái:** bộ tệp đã qua `docker compose config` và `next build` production với URL của tên miền này. **Chưa chạy `up` thật** (môi trường soạn không có Docker daemon) và repo chưa từng chạy Compose sau khi tách khỏi Agrigreen_demo_v2 (xem README, mục "Chưa kiểm"). Lần dựng đầu cần theo dõi log từng dịch vụ (mục 6).

## 1. Mô hình

Một bản ghi DNS, một chứng thư Let's Encrypt. Caddy là cổng vào duy nhất (80/443) và chia theo đường dẫn; mọi dịch vụ khác không mở cổng ra máy.

| Đường dẫn công khai | Tới | Ghi chú |
|---|---|---|
| `/` | `web-htx:3000` | Next.js standalone |
| `/api/*` | `api-platform:8080` | bỏ tiền tố: `/api/v1/me` → `/v1/me`. Web, app Flutter ghép `baseUrl + path` nên giữ được tiền tố |
| `/admin-api/*` | `api-platform:8082` | Admin API, MFA bắt buộc ở ứng dụng; tùy chọn chặn theo IP (`ADMIN_API_ALLOW_CIDR`) |
| `/auth/*` | `keycloak:8080` | Keycloak chạy `start` (production), `KC_HTTP_RELATIVE_PATH=/auth`, DB PostgreSQL riêng. `/auth/admin` và realm `master` chỉ mở cho `ADMIN_ALLOW_CIDR` |
| `/<bucket>/*` | `seaweedfs:9000` | chỉ GET/HEAD — URL tải ký sẵn (path-style, giữ Host nên chữ ký SigV4 không đổi) |

Vì web, API và Keycloak cùng một origin nên trình duyệt không cần CORS; `AG_CORS_ORIGINS` vẫn đặt bằng `PUBLIC_URL`.

Biến môi trường chính do hồ sơ demo đặt (xem `docker-compose.demo.yml`):

| Biến | Giá trị |
|---|---|
| `AG_ENV` | `staging` — không phải production (RB-7, guard kho định danh chỉ áp production) nhưng kênh gửi vẫn fail-closed |
| `AG_OIDC_ISSUER` | `https://demo-htx.mekonggreen.vn/auth/realms/agrigreen` (khớp `KC_HOSTNAME`) |
| `AG_OIDC_JWKS_URL`, `AG_KC_ADMIN_URL` | qua mạng nội bộ `http://keycloak:8080/auth/...` |
| `AG_S3_PUBLIC_ENDPOINT` | `demo-htx.mekonggreen.vn` (TLS) |
| `AG_WEB_OPS_URL`, `AG_REPORT_LINK_BASE`, `AG_KC_ACTIONS_REDIRECT_URI` | theo `PUBLIC_URL` |
| `AG_TRUSTED_PROXIES` | dải mạng compose `172.31.250.0/24` (Caddy) |
| `NEXT_PUBLIC_DEPLOY_ENV` | `staging` — bản thật, cấm giả lập, URL bắt buộc https |

## 2. Chuẩn bị

**Máy:** VPS đặt tại Việt Nam (RB-7 — dữ liệu trong lãnh thổ), Ubuntu 24.04 LTS, tối thiểu **4 vCPU, 8 GB RAM, 80 GB SSD** (Keycloak ~1 GB, PostgreSQL/PostGIS, RabbitMQ, bước `next build` cần ~2–3 GB lúc build). Thiếu RAM thì build image ở CI/máy khác rồi đẩy lên registry.

**Phần mềm:** Docker Engine + Compose plugin **≥ 2.24** (tệp demo dùng `!reset`/`!override`), `git`, `jq`, `openssl`.

**DNS:** bản ghi `A` (và `AAAA` nếu có IPv6) `demo-htx.mekonggreen.vn` → IP máy, TTL 300. Kiểm: `dig +short demo-htx.mekonggreen.vn`. Let's Encrypt cần DNS trỏ đúng **trước** khi bật Caddy.

**Tường lửa:** chỉ mở 22 (giới hạn IP quản trị), 80, 443/tcp, 443/udp (HTTP/3, tùy chọn).

```bash
sudo ufw allow from <IP_QUAN_TRI> to any port 22 proto tcp
```

```bash
sudo ufw allow 80,443/tcp && sudo ufw allow 443/udp && sudo ufw enable
```

**Quyết định cần chốt trước:** commit/tag triển khai (khuyến nghị tag trên `dev`, vd `demo-2026-11-xx`), email ACME, dải IP quản trị, có giữ TOTP cho Admin trong buổi demo hay không (mặc định giữ), thông tin hộp "Liên hệ quản trị viên".

## 3. Dựng lần đầu

1. Lấy mã:

   ```bash
   git clone --branch dev https://github.com/MEKONG-GREEN/hop-tac-xa.git && cd hop-tac-xa
   ```

2. Cấu hình không bí mật — chép rồi sửa `PUBLIC_HOST`, `PUBLIC_URL`, `ACME_EMAIL`, `ADMIN_ALLOW_CIDR`, hộp hỗ trợ:

   ```bash
   cp deploy/demo/env.example deploy/demo/.env
   ```

3. Sinh bí mật và realm theo tên miền (chạy lại an toàn, không đổi bí mật đã có):

   ```bash
   sh deploy/demo/prepare.sh
   ```

   Script gọi `scripts/gen-dev-secrets.sh` (ghi `deploy/compose/.env`, quyền 600), thêm `KEYCLOAK_DB_PASSWORD`, rồi sinh `deploy/demo/.generated/keycloak/agrigreen-realm.json`: client `agrigreen-web-ops` dùng `PUBLIC_URL`; tắt client không thuộc App HTX (`agrigreen-seed` chỉ-dev, `kn-web`, `agrigreen-web-internal`, app hiện trường, MKG, TMS); không nhập realm `agrigreen-kn-admin`.

   **Sao lưu `deploy/compose/.env` ra nơi an toàn ngoài máy** (két mật khẩu). Mất `AG_VAULT_KEK`/`AG_VAULT_BLIND_KEY` là mất dữ liệu định danh đã mã hóa.

4. Build và chạy (lần đầu build 6 image Go + web, 10–20 phút):

   ```bash
   sh deploy/demo/compose.sh up -d --build --wait
   ```

   Thứ tự tự động: `postgres` → `migrate-bootstrap` → `migrate-up` → `keycloak-db-init` → `keycloak` (nhập realm) → `identity-vault`, `api-platform`, `worker`, `outbox-publisher` → `web-htx` → `caddy` (xin chứng thư).

5. Kiểm khói:

   ```bash
   sh deploy/demo/compose.sh ps
   ```

   ```bash
   curl -fsS https://demo-htx.mekonggreen.vn/api/healthz && curl -fsS https://demo-htx.mekonggreen.vn/api/readyz
   ```

   ```bash
   curl -fsS https://demo-htx.mekonggreen.vn/auth/realms/agrigreen/.well-known/openid-configuration | jq -r .issuer
   ```

   `issuer` phải đúng `https://demo-htx.mekonggreen.vn/auth/realms/agrigreen`. Mở `https://demo-htx.mekonggreen.vn/login` thấy màn đăng nhập. Từ IP ngoài `ADMIN_ALLOW_CIDR`, `https://demo-htx.mekonggreen.vn/auth/admin/` phải trả 403.

## 4. Tài khoản và dữ liệu demo

1. **Admin đầu tiên** ([dang-nhap-app-htx.md](dang-nhap-app-htx.md) mục 3). Tenant demo là một UUID cố định ghi lại trong tài liệu vận hành (không phải bí mật):

   ```bash
   sh deploy/demo/compose.sh run --rm bootstrap-admin -tenant <uuid-tenant> -org <uuid-don-vi> -username <ten-dang-nhap> -email <email>
   ```

   Mật khẩu tạm in ra stdout **một lần**; chuyển qua kênh an toàn. Đăng nhập lần đầu → "Hoàn tất trên trang xác thực" → đổi mật khẩu, đăng ký TOTP → đăng nhập lại ở màn của web với mật khẩu + mã OTP.

2. **Dữ liệu nền** bằng giao diện Admin: danh mục đơn vị hành chính (FN-02), HTX (FN-03), tài khoản Đại diện HTX, rồi Đại diện HTX tạo Thành viên HTX (FN-08), danh mục mùa vụ, SOP. Repo chưa có script nạp dữ liệu nền (`deploy/vps/bootstrap.py` của repo gốc không được mang sang) — nên viết một script nạp qua API cho máy demo để dựng lại nhanh.

3. **Tài khoản trình diễn:** một tài khoản cho mỗi vai (Admin, Đại diện HTX, Thành viên HTX), mật khẩu riêng, ghi trong két mật khẩu của nhóm. Không dùng `scripts/dev-users.sh` (chỉ chạy với Keycloak localhost).

## 5. App di động (tùy chọn)

App Flutter trỏ về máy demo bằng `--dart-define` (client OIDC `agrigreen-htx-app`, redirect `vn.agrigreen.htx:/oauth2redirect` đã có trong realm):

```bash
cd apps/htx-app && flutter build apk --release --dart-define=AG_API_BASE=https://demo-htx.mekonggreen.vn/api --dart-define=AG_OIDC_ISSUER=https://demo-htx.mekonggreen.vn/auth/realms/agrigreen
```

Thêm `--dart-define=AG_TILE_URL=...` nếu dùng nhà cung cấp tile trong nước; FCM chỉ bật khi có `AG_FCM_*`.

## 6. Vận hành

| Việc | Lệnh / cách làm |
|---|---|
| Xem log | `sh deploy/demo/compose.sh logs -f --tail=200 api-platform worker keycloak caddy` |
| Cập nhật phiên bản | `git fetch && git checkout <tag mới>` → `sh deploy/demo/compose.sh up -d --build --wait` (`migrate-up` chạy lại tự động). Sao lưu DB trước |
| Lùi phiên bản | Migration forward-only: chỉ lùi mã khi bản cũ chạy được trên schema mới; nếu không thì khôi phục bản sao lưu trước khi nâng |
| Sao lưu tối thiểu | hằng ngày `docker exec htx-demo-postgres-1 pg_dumpall -U postgres \| gzip > /backup/htx-$(date +%F).sql.gz` + sao lưu volume `htx-demo_objects`; chép ra ngoài máy. Hồ sơ pgBackRest đầy đủ: [backup.md](backup.md) |
| Chứng thư | Caddy tự gia hạn; dữ liệu ACME ở volume `htx-demo_caddy_data` |
| Giám sát | kiểm uptime ngoài máy trên `/api/healthz` và `/login` mỗi 1–5 phút |
| Email | Mặc định tắt (fail-closed). Bật: điền `AG_SMTP_*` (`deploy/demo/.env`, mật khẩu ở `deploy/compose/.env`) cho Keycloak; worker cần thêm `AG_SMTP_*` vào hồ sơ demo |

**Realm chỉ nhập một lần.** Đổi tên miền hoặc client sau khi đã chạy: sửa trong Admin Console (từ IP quản trị) hoặc xóa DB `keycloak` rồi dựng lại (mất mọi tài khoản).

## 7. Danh mục kiểm an toàn trước buổi demo

- [ ] Chỉ 80/443 (và 22 giới hạn IP) mở ra Internet; `sh deploy/demo/compose.sh ps` chỉ Caddy có cổng publish.
- [ ] `/auth/admin` trả 403 từ IP ngoài; mật khẩu `admin` của Keycloak (KEYCLOAK_ADMIN_PASSWORD) chỉ trong két.
- [ ] `AG_MFA_DISABLED=0` (trừ khi đã quyết bỏ TOTP cho buổi demo — chỉ hiệu lực vì `AG_DEPLOY_ENV=demo`).
- [ ] Client `agrigreen-seed` (direct grant chỉ-dev) đang tắt.
- [ ] `deploy/compose/.env` quyền 600, đã sao lưu ngoài máy.
- [ ] Không có dữ liệu cá nhân thật; dữ liệu demo đánh dấu rõ.

## 8. Hạn chế đã biết (review nhánh `dev`, 17/11/2026)

- Compose của repo chưa từng chạy sau khi tách; realm còn vai và client của KN/MKG (đã tắt client ở hồ sơ demo); `rabbitmq-init` vẫn tạo vhost `mkg.internal` (vô hại); màn đăng nhập và thanh bên còn nhãn "Khuyến nông số" (README, mục "Còn giữ tạm").
- Tài khoản chỉ là Thành viên HTX nhận 403 khi bấm "Đọc tất cả"/đánh dấu đã đọc thông báo và khi đăng ký push token: `memberWriteGuard` chặn mọi route ghi, `member_write_routes` đang rỗng.
- Chưa có kịch bản e2e cho HTX; nên chạy tay luồng đăng nhập ba vai sau mỗi lần cập nhật.

## 9. Phương án thay thế

- **Chỉ giao diện (nhanh nhất):** bản giả lập `npm run build:mock` (`NEXT_PUBLIC_DEPLOY_ENV=demo`, `NEXT_PUBLIC_API_MOCK=1`) chạy một container Next.js sau Caddy, không cần backend/Keycloak. Bỏ qua đăng nhập, dữ liệu tổng hợp trong trình duyệt — chỉ hợp để trình diễn màn hình, không để thử nghiệm với người dùng.
- **Tên miền con** (`api.`, `auth.`, `admin.` dưới một tên miền): như mô hình của repo gốc, cần thêm bản ghi DNS và chứng thư; không cần thiết cho demo.
