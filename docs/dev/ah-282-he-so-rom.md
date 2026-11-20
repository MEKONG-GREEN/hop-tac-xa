# AH-282 — hệ số rơm theo giống (FN-11)

Theo quyết định PO ngày 15/11/2026, cấu hình này chỉ phục vụ CT-5 của bộ
thống kê AH-287. Không sửa công thức dự báo Thu gom phụ phẩm hoặc chi tiết vụ.
Dependency AH-272 và danh mục giống AH-2 đã có trên `dev-agent`.

Admin/MFA thêm, sửa và xem lịch sử trên web; không có xóa. Quyền theo ma trận
FN-11. Danh mục đi qua `masterdata/port`, chỉ giống thuộc loại cây có `is_rice`.
Không thêm cấu hình mới cho giống bị ẩn; giữ hệ số và lịch sử đã có.
Một hệ số hiện hành mỗi giống, 0–100%, tối đa 4 chữ số thập phân. Gợi ý
30–40% không chặn lưu. Chưa cấu hình hoặc mốc trước lần cấu hình đầu dùng 35%.

`pf_coop.straw_factor_event` là ledger chỉ ghi thêm, một stream mỗi giống,
có giá trị trước/sau, actor và thời điểm hiệu lực do DB sinh dưới khóa giống.
Khóa idempotency được kiểm payload/actor; retry không tạo thêm lịch sử.
Sửa dùng expected revision để chặn ghi đè. Không I/O port trong transaction.
Danh mục giống đã có lịch sử hệ số được xem là đang dùng, không xóa cứng.

Admin API `/v1/cropcycle/straw-factors`: GET danh sách, POST thêm;
`/:variety_code/revisions` POST sửa; `/:variety_code/history` GET lịch sử
phân trang bằng `before_revision`. Không có API xóa.

`cooperative.Module.StrawFactors()` trả port tra phần trăm hiện tại hoặc
tại mốc UTC; tenant context bắt buộc. AH-287 phải dùng mốc hoàn thành vụ
của AH-152 cho vụ đã hoàn thành, còn vụ đang chạy dùng hiện tại, rồi chia
phần trăm cho 100 khi nhân sản lượng bằng decimal. Ticket này cung cấp nguồn
và lịch sử; không tự coi dashboard hiện tại đã thực hiện CT-5.
