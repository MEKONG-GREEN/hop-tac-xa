# hop-tac-xa — App Hợp tác xã

Backend Go (modular monolith) + PostgreSQL 17/PostGIS, web Next.js (`apps/web-htx`) và app Flutter (`apps/htx-app`) cho Hợp tác xã.

## Nguồn gốc

Repo này là **bản chụp (snapshot)** phần mã Hợp tác xã của [MEKONG-GREEN/Agrigreen_demo_v2](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2) tại nhánh `dev`, commit `360276fbd6835f73dbba26f339f008257636eb36` (11/11/2026). Không mang lịch sử git; tra lịch sử ở repo gốc. Quyết định tách: [ADR-0059](docs/adr/0059-tach-he-thong-hop-tac-xa.md).

So với repo gốc:

- **Bỏ:** hệ Khuyến nông (`internal/kn`), MKG (`internal/mkg`, cụm `mkg`/`ts`), mô phỏng chọn Hub + optimizer, `pf/forms`, `pf/export`, `pf/extdata`, `pf/guestlink`, web nội bộ, app hiện trường, cổng tích hợp, triển khai k8s/VPS.
- **Giữ:** `pf/cooperative`, `pf/cropcycle` và các module nền chúng cần (`accounts`, `party`, `geo`, `masterdata`, `notify`, `reports`, `sync`, `content`, `evidence`, `workflow`, `metrics`, `metering`, `apiclient`, `kref`, `mechanization`, `mrv`, `waterway`), `internal/shared`, kho định danh.
- **Viết lại:** gốc lắp ráp `cmd/api-platform/assembly` và `cmd/worker` chỉ nối module ở trên; danh mục vai dùng bản gốc của nền tảng (`accport.NativeRoleCatalog`).

## Còn giữ tạm — phải gỡ ở các gói sau

| Phần | Vì sao còn | Gỡ khi |
|---|---|---|
| `internal/kn/{contact,extension,hrm,price}`, `internal/platform/{forms,export,guestlink,simulation}` — mỗi thư mục chỉ có `module.yaml` + `doc.go` | Chuỗi migration `migrations/pf` xen kẽ mọi module và là forward-only; vai DB và view công bố suy từ `module.yaml` (`modreg.DBOnlyModules`) | Gộp lại (squash) migration cho hệ HTX |
| Schema `pf_extension`, `pf_price`, `pf_hrm`, `pf_contact`, `pf_forms`, `pf_export`, `pf_guestlink`, `pf_simulation` | Như trên — bảng vẫn được tạo nhưng không mã nào dùng | Như trên |
| `apps/web-htx/src/kn/*` (23 tệp), `src/cgh/*` (3 tệp), `src/api/mock/kn*`, `mock/forms` và fixture KN (khoảng 110 tệp) | Vỏ web (đăng nhập, ngữ cảnh phiên, thanh bên) và lớp giả lập dựng trên phiên/ngữ cảnh của KN | Viết vỏ web theo ba vai trò của BRD HTX |
| Nhãn "Khuyến nông số" ở màn đăng nhập và thanh bên | Chưa có bộ nhận diện App HTX | Như trên |
| `contracts/openapi`, `contracts/events` và mã sinh `apps/web-htx/src/api/generated` còn mô tả API/sự kiện của KN | Hợp đồng gộp chưa tách theo module giữ lại; route KN không còn được phục vụ | Gói bảng bề mặt theo BRD HTX |
| Realm Keycloak `deploy/compose/keycloak/*` còn vai và client của KN/MKG | Chưa tách realm | Gói hồ sơ Compose chỉ-HTX |
| `rabbitmq-init` trong Compose còn tạo vhost `mkg.internal` | Chưa chạy được Compose để kiểm sau khi sửa | Như trên |

## Trạng thái kiểm tra lúc tạo bản chụp

Đã chạy và đạt: `gofmt`, `go build ./...`, `go vet ./...`, `modgen -check`, `archlint`, `migratecheck`, `contractcheck`, `docscheck`, `mfaflagcheck`, `go test ./...` (128 gói — **test cần PostgreSQL bị SKIP**), web-htx: `check:api`, `eslint`, `tsc`, `vitest` (358 ca), `build:mock`; mở thử `/htx/dashboard`, `/htx/mua-vu`, `/htx/nong-ho` ở chế độ giả lập.

**Chưa kiểm** (môi trường tạo bản chụp không có PostgreSQL 17, Docker daemon và Flutter): `migrate up` trên cụm trống, mọi test tích hợp DB, khởi động `api-platform`/`worker` thật, Docker Compose, `flutter analyze`/`flutter test`, Playwright e2e. CI của repo (`.github/workflows/ci.yml`) chạy các phần này.

Test đã chuyển đối tượng thử cho khớp phạm vi repo: test sổ append-only và job vận hành (`internal/shared/ledger`, `internal/shared/opsjobs`) chạy trên sổ `pf_party.consent` thay cho `mkg_fieldops.roll_count`; fixture chuyển tenant của `party/handler` dùng schema/vai của `pf/content` thay cho `pf/forms`.

Đã bỏ vì chỉ thử cụm `mkg`/`ts`: test vai giám sát và `ts_weigh` trong `migrations/`, `TestLedgerAddColumnUpgradeMKG`, `TestKRefInMKGCluster_W25`, `TestOutboxKindRoutesToOwnBroker`.

Đã bỏ vì khung kiểm thử dựng trên KN/MKG: `tests/e2e`, `tests/acceptance`, `tests/notify`, `tests/reports` và các test canh gác đếm đủ vai/module của hệ gốc. Cần viết lại kịch bản đầu–cuối cho HTX.

## Cấu trúc

| Thư mục | Nội dung |
|---|---|
| `cmd/` | `api-platform`, `worker`, `outbox-publisher`, `migrate`, `identity-vault` |
| `internal/shared/` | hạ tầng dùng chung: `db`, `tenant`, `authz`, `idempotency`, `outbox`, `problem`, `modreg`, `pgtest`, … |
| `internal/platform/` | module nghiệp vụ và nền tảng |
| `internal/vault/` | kho định danh (PII) |
| `migrations/` | SQL forward-only: `shared/`, `pf/`, `vault/` |
| `contracts/` | OpenAPI, view `_pub`, sự kiện |
| `apps/web-htx` | web (Next.js) |
| `apps/htx-app`, `packages/agrigreen_mobile` | app Flutter và lõi di động |
| `app/` | khai báo ứng dụng `HTX`, `PF` |
| `tools/` | cổng CI: `archlint`, `modgen`, `migratecheck`, `contractcheck`, `docscheck`, `mfaflagcheck`, `docsql` |
| `deploy/compose` | Docker Compose cho máy phát triển |
| `docs/` | ADR, quy ước, runbook |

## Chạy

Xem [docs/dev-setup.md](docs/dev-setup.md) (viết cho repo gốc; tên thư mục web đã đổi thành `apps/web-htx`). Web không cần backend:

```bash
cd apps/web-htx
npm ci
npm run dev:mock
```

Cổng trước khi gộp: xem [AGENTS.md](AGENTS.md).
