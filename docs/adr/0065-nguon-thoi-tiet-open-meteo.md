# ADR-0065: Nguồn thời tiết Open-Meteo qua module `pf/weather`

**Trạng thái:** Đề xuất — mã đã có ở `dev-agent`; bật ở production chờ chủ sản phẩm duyệt chỗ lệch lưới so với
ADR-0007 (mục 4). **Ngày:** 23/11/2026 (AH-224, AH-225, AH-226; epic AH-217). **Nguồn:** chủ repo chọn "Trung lập +
Open-Meteo" ngày 23/11/2026.

**Liên quan:** [ADR-0007](0007-nha-cung-cap-trong-lanh-tho.md) (nguồn ngoài lãnh thổ),
[ADR-0028](0028-vai-dang-nhap-theo-don-vi-trien-khai.md) (vai `r_weather`), [runbook](../runbooks/thoi-tiet.md),
[vai trò App HTX](../conventions/vai-tro-app-htx.md).

## Bối cảnh

BRD Thời tiết & Thu gom phụ phẩm có ba chức năng: FN-TT-03 (Admin kết nối API thời tiết), FN-TT-01 (thẻ Thời tiết
hôm nay trên Trang chủ) và FN-TT-02 (Lịch thời tiết dương lịch và âm lịch). TECH-TP-02 và TECH-TP-03 yêu cầu máy chủ
gọi nhà cung cấp theo chu kỳ, lưu đệm, app không gọi trực tiếp. BRD không chỉ định nhà cung cấp.

ADR-0007 cho phép nguồn thời tiết nước ngoài khi không gửi định danh, tọa độ làm thô theo lưới (tối thiểu 0,05°), và
mỗi nguồn có ADR riêng. Repo gốc thực hiện qua proxy `pf/extdata`; repo này đã bỏ `pf/extdata` khi tách
([README](../../README.md#nguồn-gốc)).

## Quyết định

1. **Module `pf/weather`** (`internal/platform/weather`, `enable: per_tenant`, schema `pf_weather`, phụ thuộc
   `pf/cooperative` qua port để lấy tọa độ trụ sở HTX). Worker chạy job `weather_fetch` mỗi 5 phút, lấy số liệu cho
   tenant đến lượt theo chu kỳ Admin chọn (30, 60, 120, 180, 360 phút) và lưu đệm. Web và app chỉ đọc
   `GET /v1/weather/today`, `GET /v1/weather/calendar`. Không gọi mạng trong giao dịch DB.
2. **Port trung lập với nhà cung cấp.** `app.Provider` trả mô hình chung (hiện tại, theo giờ, theo ngày). Có hai bộ chuyển:
   Open-Meteo (`provider/openmeteo`) và nguồn giả (`provider/fake`, chỉ ngoài production, bật bằng
   `AG_WEATHER_FAKE=true`). Thêm nhà cung cấp là thêm một bộ chuyển và một giá trị `provider` (CHECK ở migration).
3. **Gửi tối thiểu.** Mỗi lần gọi chỉ gửi vĩ độ, kinh độ đã làm thô, danh sách biến, múi giờ `Asia/Ho_Chi_Minh` và khóa
   API nếu Admin nhập (gói thương mại). Không gửi tên hay mã HTX, mã tenant, token hay header của người dùng. Không đi
   theo chuyển hướng; User-Agent cố định `agrigreen-weather/1`.
4. **Lưới 0,01° và bán kính dùng chung 1 km (BR-02 của FN-TT-03).** Tọa độ trụ sở làm thô về 0,01° (khoảng 1,1 km).
   HTX cách một điểm đã có dưới 1 km dùng chung điểm đó, nên nhà cung cấp không biết có bao nhiêu HTX.
   **Lệch ADR-0007** (tối thiểu 0,05°), cần chủ sản phẩm duyệt:
   - Lưới của ADR-0007 nhằm không lộ vị trí thửa ruộng hoặc nông hộ.
   - Trụ sở HTX là địa chỉ đăng ký của tổ chức, đã công khai, không phải dữ liệu cá nhân.
   - Ô 0,05° (khoảng 5,5 km) có thể tách hai HTX cách nhau dưới 1 km sang hai ô khác nhau, trái BR-02.
   - Nếu chủ sản phẩm không duyệt, đổi `domain.GridDecimals` và `Snap` sang bước 0,05°. Phải sửa mã; dữ liệu đã lưu
     không đổi.
5. **Khóa production theo kiểu fail-closed.** Ở production, Open-Meteo chỉ được dùng khi đủ ba điều kiện:
   - `AG_WEATHER_OPEN_METEO_URL` là https;
   - máy chủ của URL đó có trong `AG_NONPII_ALLOWLIST` (guard RB-7, tiền tố `AG_WEATHER_`);
   - `AG_WEATHER_SOURCE_ADR` được đặt (ghi `ADR-0065` sau khi được duyệt).

   Thiếu một điều kiện thì nguồn tắt: "Kiểm tra kết nối" báo lỗi, job không lấy số liệu. Nguồn giả bị cấm ở production.
6. **Khóa API.** Khóa được mã hóa AES-256-GCM bằng `AG_WEATHER_SECRET_KEY`, AAD là `tenant_id`, lưu trong sổ cấu hình
   chỉ ghi thêm `pf_weather.provider_config` (`shared.apply_ledger`). API chỉ trả `••••` cùng 4 ký tự cuối (khóa dưới 8
   ký tự chỉ hiện 1 ký tự). Thiếu `AG_WEATHER_SECRET_KEY` thì không lưu được khóa (`503 weather.secret_unavailable`);
   Open-Meteo bản miễn phí vẫn chạy không cần khóa.
7. **Lỗi nguồn.**
   - Sau 3 lần lỗi liên tiếp, worker phát `pf.weather.fetch.failing` một lần; `pf/notify` báo Admin
     (`weather_fetch_failing`).
   - Vượt hạn mức (429, "limit exceeded") thì giãn tần suất về 3 giờ/lần tới 00:00 hôm sau, giờ Việt Nam
     (FN-TT-03/EX-03); tần suất Admin chọn từ 3 giờ trở lên thì giữ nguyên.
   - Trong lúc lỗi, thẻ vẫn hiện số liệu đã lưu kèm "Cập nhật lúc hh:mm dd/mm" (FN-TT-01/EX-01).
8. **Số liệu ngày đã qua (FN-TT-03/BR-05, hiển thị ở FN-TT-02).**
   - Nhà cung cấp cho số liệu quan trắc: lưu `observed`, nhãn "Thực đo".
   - Không có quan trắc: mỗi lần lấy trước 23:30 giờ Việt Nam ghi đè số liệu của hôm đó. Lần lấy cuối cùng trước 23:30
     thành `final_forecast`, nhãn "Dự báo cuối ngày". Các lần lấy từ 23:30 trở đi không ghi vào ngày đó.
   - Dòng `observed` không bị ghi đè.
   - Open-Meteo trả mô hình phân tích lại, không phải trạm đo, nên bộ chuyển Open-Meteo luôn trả `Observed = false`:
     mọi ngày đã qua đều mang nhãn "Dự báo cuối ngày".
9. **Đơn vị và làm tròn ở máy chủ (FN-TT-01/BR-04).**
   - °C và km/h là số nguyên; lượng mưa 1 chữ số thập phân; % là số nguyên.
   - Hướng gió là 8 chữ viết tắt tiếng Việt; mô tả thời tiết đổi từ mã WMO sang tiếng Việt.
   - Buổi trong ngày: Đêm 00–06, Sáng 06–12, Chiều 12–18, Tối 18–24.
10. **Âm lịch** tính ở máy chủ (`internal/shared/lunar`, thuật toán Hồ Ngọc Đức, GMT+7) và trả kèm mỗi ngày. Web có bản
    TypeScript tương đương (`src/lib/lunar.ts`) cho mock và kiểm thử. App chỉ hiển thị giá trị máy chủ trả.
11. **Ghi nguồn.** Thẻ và lịch ghi "Nguồn: Open-Meteo.com" theo giấy phép CC BY 4.0 của dữ liệu Open-Meteo. Bản miễn phí
    của Open-Meteo chỉ dành cho mục đích phi thương mại; dùng thương mại thì Admin nhập khóa của gói trả phí.
    - Không đặt `AG_WEATHER_OPEN_METEO_URL`: bộ chuyển tự chọn `api.open-meteo.com` khi không có khóa và
      `customer-api.open-meteo.com` khi có khóa.
    - Ở production URL bắt buộc và dùng đúng URL đã đặt, nên người vận hành đặt URL khớp với gói (xem runbook).

## Hệ quả

- Chưa có quyết định duyệt thì production không có thời tiết (thẻ báo "Chưa có số liệu thời tiết"). Local/Dev dùng nguồn
  giả hoặc Open-Meteo miễn phí.
- Phạm vi theo vai:
  - Admin xem mọi HTX bằng bộ lọc trên web và cấu hình kết nối trên web.
  - Đại diện HTX và Thành viên HTX chỉ xem HTX của mình, trên web và app.
  - Nông dân không có trong BRD: máy chủ trả 403, app ẩn thẻ.
- Đổi nhà cung cấp không đổi API cho web và app.
