// Package extension chỉ còn là KHAI BÁO DB (module.yaml: schema và vai r_extension) trong repo hop-tac-xa.
//
// Chuỗi migration cụm pf xen kẽ mọi module và là forward-only, nên schema của module này vẫn được tạo để chuỗi chạy
// nguyên vẹn; vai nhóm của nó suy từ module.yaml (internal/shared/dbadmin). Mã nghiệp vụ của module KHÔNG có ở đây và
// không đơn vị triển khai nào ghép nó. Gỡ hẳn khi gộp lại (squash) migration cho hệ HTX.
package extension
