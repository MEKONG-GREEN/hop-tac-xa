# AGENTS.md — hướng dẫn cho người và AI agent

Repo này tách từ Agrigreen_demo_v2 (xem [README.md](README.md#nguồn-gốc)). Quy ước mã giữ như repo gốc; chi tiết ở [docs/conventions/](docs/conventions/README.md) và [docs/dev/](docs/dev/modules.md).

## Ranh giới

- Mỗi module có `module.yaml` sinh bằng `go run ./tools/modgen`; chỉ import module khác qua `port/`; đọc dữ liệu module khác qua view `_pub` có hợp đồng trong `contracts/pubview/`.
- Không thêm mã vào các thư mục chỉ-DB (`modreg.DBOnlyModules`); danh sách đó và `tools/archlint/modgraph_exceptions.yaml` chỉ được thu nhỏ.
- Không commit bí mật: `.env`, chứng thư, mật khẩu sinh bằng `scripts/gen-dev-secrets.*` vào thư mục đã gitignore. Không ghi đường dẫn riêng của máy vào mã hay tài liệu.

## Nhánh và commit

- Một nhánh mỗi gói việc, tạo từ `dev`; gộp vào `dev` qua Pull Request. Không push thẳng `main`.
- Commit message tiếng Việt, tiền tố mã gói.

## Tệp sinh — không sửa tay

| Tệp | Lệnh sinh lại |
|---|---|
| `**/module.yaml`, `internal/shared/modreg/*_gen.go` | `go run ./tools/modgen` (kiểm: `-check`) |
| `apps/web-htx/src/api/generated/*`, mock/OpenAPI gộp của web | `npm run gen:api` trong `apps/web-htx` (kiểm: `npm run check:api`) |

## Migration

- Tên theo mốc UTC: `go run ./tools/migratecheck -new <cụm>_<module>_<tên>`. Forward-only: không sửa tệp đã gộp.
- RLS theo tenant cho mỗi bảng qua `shared.apply_tenant_rls`; sổ append-only qua `shared.apply_ledger`.

## Cổng

Trước mọi lệnh `go`: `export GOTOOLCHAIN=$(awk '/^toolchain/{print $2}' go.mod)` rồi `go version`.

```bash
gofmt -l $(git ls-files '*.go' | grep -v /testdata/)    # phải rỗng
go build ./...
go vet ./...
go run ./tools/archlint ./...
go run ./tools/modgen -check
go run ./tools/migratecheck -base origin/dev migrations
go run ./tools/contractcheck
go run ./tools/docscheck
golangci-lint run ./...
```

Web (`apps/web-htx`): `npm run check:api`, `npm run lint`, `npx tsc --noEmit`, `npm test`, `npm run build:mock`.

Flutter (`apps/htx-app`, `packages/agrigreen_mobile`): `dart format --output=none --set-exit-if-changed lib test`, `flutter analyze --fatal-infos`, `flutter test`.

Test DB cần `AG_TEST_PG_ADMIN_URL` (PostgreSQL 17 + PostGIS + pgRouting, image ở `deploy/compose/postgres`). Thiếu biến thì test DB **SKIP âm thầm** — SKIP tính là trượt; xác nhận bằng `go test -v` thấy `--- PASS`.

## Lỗi bắt buộc tránh

1. Không gọi port module khác, không I/O mạng bên trong DB transaction ([quy ước](docs/conventions/transactions-and-io.md)).
2. Cấu hình fail-closed: thiếu SMTP/FCM/URL thì kênh tắt, không báo "Đã gửi" giả ([quy ước](docs/conventions/fail-closed-config.md)).
3. Một `Idempotency-Key` cho mỗi thao tác người dùng ([quy ước](docs/conventions/idempotency.md)).
4. Worker kiểm phụ thuộc khi khởi động; nhận việc gửi nguyên tử.
5. RLS fail-closed: policy không bỏ qua kiểm khi bảng chiếu rỗng.
6. Kiểm số ở server, không chỉ ở UI.
7. Không dùng float cho tiền/khối lượng; ID là UUIDv7; PII chỉ ở `vault/identity`.
