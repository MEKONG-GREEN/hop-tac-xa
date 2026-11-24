# Thời tiết HTX (pf/weather)

Cách bật và vận hành nguồn thời tiết của App HTX ([ADR-0065](../adr/0065-nguon-thoi-tiet-open-meteo.md)). Nội dung gồm
biến môi trường, job `weather_fetch` và xử lý khi nguồn lỗi. Chức năng: FN-TT-01 (Thời tiết hôm nay), FN-TT-02 (Lịch
thời tiết), FN-TT-03 (Kết nối API thời tiết; Admin cấu hình trên web ở `/quan-tri/ket-noi-thoi-tiet`).

## 1. Biến môi trường (`api-platform` và `worker`)

Hai dịch vụ dùng cùng bộ biến: `api-platform` kiểm tra kết nối và lưu cấu hình; `worker` lấy số liệu.

| Biến | Ý nghĩa |
|---|---|
| `AG_WEATHER_SECRET_KEY` | Khóa AES-256 (base64 chuẩn, đúng 32 byte; `head -c 32 /dev/urandom \| base64`) dùng để mã hóa khóa API nhà cung cấp. Sai định dạng thì dịch vụ không khởi động. Thiếu thì Admin không lưu được khóa API (`503 weather.secret_unavailable`); Open-Meteo bản miễn phí vẫn chạy. Hai dịch vụ phải dùng **cùng** khóa. Đổi khóa thì các khóa API đã lưu không giải mã được nữa, và Admin phải nhập lại khóa API. |
| `AG_WEATHER_OPEN_METEO_URL` | Endpoint Open-Meteo. **Bắt buộc ở production**, phải là `https`, và máy chủ của URL phải có trong `AG_NONPII_ALLOWLIST`. Gói miễn phí (phi thương mại): `https://api.open-meteo.com`. Gói thương mại: `https://customer-api.open-meteo.com`, đặt cùng khóa API do Admin nhập. Ngoài production, để trống thì tự chọn theo việc có khóa hay không. |
| `AG_WEATHER_SOURCE_ADR` | **Bắt buộc ở production**: ghi `ADR-0065` khi ADR đã được duyệt. Thiếu thì nguồn tắt. |
| `AG_WEATHER_FAKE` | `true` cho phép chọn "Nguồn thử nghiệm" (số liệu giả, cố định). Production luôn bỏ qua biến này. |
| `AG_NONPII_ALLOWLIST` | Thêm máy chủ Open-Meteo, ví dụ `api.open-meteo.com` hoặc `customer-api.open-meteo.com`. Xem [residency.md](residency.md). |

Ở production, thiếu `AG_WEATHER_OPEN_METEO_URL` hoặc `AG_WEATHER_SOURCE_ADR`, hoặc URL không phải `https`, thì nguồn
tắt mà dịch vụ vẫn chạy:
- "Kiểm tra kết nối" trả `422 weather.connection_failed`.
- Job không lấy được số liệu.
- Thẻ thời tiết báo "Chưa có số liệu thời tiết".

Riêng trường hợp URL trỏ tới máy chủ ngoài `AG_NONPII_ALLOWLIST`: guard RB-7 dừng dịch vụ ngay lúc khởi động.

**Docker Compose Local:**
1. Chạy `scripts/gen-dev-secrets` để bổ sung `AG_WEATHER_SECRET_KEY` vào `deploy/compose/.env`.
2. Tạo lại `api-platform` và `worker`.

Compose đặt sẵn `AG_WEATHER_FAKE=true`.

## 2. Cấu hình lần đầu (Admin, web)

1. Vào **Kết nối thời tiết**. Chọn nguồn, tần suất cập nhật (30 phút đến 6 giờ, mặc định 60 phút) và số ngày dự báo
   (1–14, mặc định 7). Nhập khóa API nếu dùng gói thương mại.
2. Bấm **Kiểm tra kết nối**. Máy chủ gọi thử tại tọa độ HTX đầu tiên có tọa độ (nếu chưa có HTX nào có tọa độ thì dùng
   Cần Thơ).
3. Bấm **Lưu**. Máy chủ kiểm tra lại kết nối trước khi ghi, và chỉ lưu khi kết nối được.

Màn hình chỉ hiện khóa dạng `••••` cùng 4 ký tự cuối. Bỏ trống ô khóa thì giữ khóa cũ. Đánh dấu "Bỏ khóa API đang lưu" thì bỏ khóa.

Mỗi lần lưu là một phiên bản mới trong `pf_weather.provider_config` (sổ chỉ ghi thêm). Gửi lại cùng `Idempotency-Key` thì
nhận lại kết quả cũ.

HTX cần có tọa độ trụ sở (sửa ở màn HTX). HTX chưa có tọa độ thì thẻ báo "Chưa có vị trí để xem thời tiết. Liên hệ
Admin để cập nhật."

## 3. Job `weather_fetch` (`cmd/worker`, leader)

- Job chạy mỗi 5 phút và khi khởi động; giới hạn 4 phút mỗi lần.
- Danh sách tenant đã cấu hình đọc qua vai `ag_ops_jobs` (chỉ cột `tenant_id` của `provider_config`). Ghi bằng
  `r_weather`.
- Mỗi tenant đến lượt theo tần suất đã chọn:
  1. Gom HTX đang hoạt động có tọa độ thành điểm lưới 0,01°; HTX cách một điểm đã có dưới 1 km dùng chung điểm đó.
  2. Gọi nhà cung cấp cho từng điểm, ngoài giao dịch DB.
  3. Lưu bản dự báo và số liệu theo ngày.
- Chạy riêng job: `worker -jobs weather_fetch`. Không chọn job này thì không có số liệu mới, nhưng thẻ vẫn hiện bản đã
  lưu kèm "Cập nhật lúc …".
- Log có `event=job.weather_fetch`, các trường `tenants`, `due`, `locations`, `failed`, `notified`, `outcome`.

## 4. Khi nguồn lỗi

| Hiện tượng | Nguyên nhân | Xử lý |
|---|---|---|
| Admin nhận thông báo khẩn "Không lấy được số liệu thời tiết" | Lỗi 3 lần liên tiếp (sự kiện `pf.weather.fetch.failing`, chỉ báo một lần cho mỗi đợt lỗi) | Xem log `job.weather_fetch`. Bấm Kiểm tra kết nối trên màn Kết nối thời tiết. Kiểm tra mạng ra Open-Meteo và `AG_NONPII_ALLOWLIST`. |
| Lý do "vượt hạn mức" | Gói hết lượt (HTTP 429 hoặc "limit exceeded") | Job tự dừng tới 00:00 giờ Việt Nam. Muốn lấy lại sớm: giảm tần suất, hoặc nhập khóa gói thương mại rồi Lưu. |
| Lý do "khóa API không hợp lệ" | 401/403 từ nhà cung cấp | Nhập lại khóa, Kiểm tra kết nối, Lưu. |
| `503 weather.secret_unavailable` khi Lưu | Thiếu `AG_WEATHER_SECRET_KEY`, hoặc khóa đã đổi | Đặt lại biến (cùng giá trị ở hai dịch vụ). Nếu đã đổi khóa thì nhập lại khóa API. |

Trong lúc lỗi, web và app vẫn hiện bản đã lưu kèm "Cập nhật lúc hh:mm dd/mm". App ngoại tuyến cũng dùng bản đã lưu trên
máy.

## 5. Số liệu ngày đã qua

- Mỗi lần lấy trước 23:30 giờ Việt Nam ghi đè số liệu của hôm đó. Lần cuối cùng trước 23:30 là "Dự báo cuối ngày"
  (FN-TT-03/BR-05). Các lần lấy từ 23:30 trở đi không ghi vào ngày đó.
- Nếu nhà cung cấp có số liệu quan trắc thì lưu "Thực đo", và bản dự báo không ghi đè dòng "Thực đo".
- Open-Meteo không có số liệu quan trắc, nên mọi ngày đã qua mang nhãn "Dự báo cuối ngày".
- Ngày không có lần lấy thành công nào trước 23:30 (ví dụ worker dừng cả ngày) thì không có số liệu trên lịch.
