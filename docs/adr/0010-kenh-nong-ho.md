# ADR-0010: Kênh nông hộ: Flutter cho hiện trường, Zalo Mini App chỉ online

**Trạng thái:** Chấp nhận — 30/09/2026, theo quyết định của chủ sản phẩm trong phiên làm việc · **Ngày:** 27/09/2026

## Bối cảnh (X3)
Backlog App HTX đòi nông dân: đăng nhập offline, vẽ ranh giới offline, ảnh có watermark GPS, nhật ký "chờ đồng bộ". Zalo Mini App không đáp ứng được các yêu cầu này.

## Đề xuất
- Nông dân và cán bộ hiện trường dùng **app Flutter** (`apps/field-app`) cho các luồng cần offline.
- Zalo Mini App chỉ cho các chức năng online: tin tức, thời tiết, xem vụ, nhận thông báo.
- Không gửi mật khẩu tạm qua email (GIS, CGH, HTX). Kích hoạt tài khoản bằng liên kết một lần của Keycloak. Nông dân kích hoạt bằng QR hoặc liên kết do quản lý HTX phát (g2-conventions §1).
- Vai quản trị (đánh dấu `[MFA]` trong realm) bắt buộc MFA: Admin API chặn token không có yếu tố thứ hai, và tài khoản mới có vai này phải cấu hình TOTP. Vai hiện trường đăng nhập ngoại tuyến bằng PIN trên app Flutter.
- Ranh giới hiện trường: thiết bị `android` hoặc `ios` (app Flutter). Zalo Mini App không có thiết bị đồng bộ.
- Bổ sung AH-99, KH-16 chốt 13/11/2026: Web HTX chụp trực tiếp và mã hóa ảnh trước khi tải lên. Thiết bị `web` chỉ được đăng ký bởi người có quyền ghi nhật ký hộ hiện tại (Admin cần MFA); chỉ tải ảnh và push `household_task_record`, không pull hoặc push thực thể hiện trường khác. Không đổi phạm vi của Zalo Mini App.

## Triển khai
Đối chiếu từng cam kết với mã và kiểm thử: [trien-khai-g1.md](trien-khai-g1.md#adr-0010-kênh-nông-hộ).
