// SINH TỪ contracts/htxroles/htx-roles.v1.json — không sửa tay; chạy `npm run gen:api` trong apps/web-htx.
// Ma trận quyền Mục V (FN-05): docs/conventions/vai-tro-app-htx.md.
export const HTX_ROLE_MATRIX = {
  "version": 1,
  "source": "BRD_App_HTX_QuanTriPhanQuyen_HoSoHTX_ThongKeBaoCao_v3.0 — Mục V (FN-05, ADM-01); quyết định nhóm dự án 17/11/2026 (AH-272); BRD Kế hoạch mùa vụ v3.9 Mục VI — dòng KHMV-* (AH-39, KH-02, KH-28); BRD Thời tiết & Thu gom phụ phẩm — dòng TTPP-FN-TT-* (AH-224..AH-226)",
  "actions": {
    "C": "tạo",
    "R": "xem",
    "U": "sửa",
    "D": "xóa",
    "L": "khóa hoặc mở khóa",
    "P": "đặt lại mật khẩu",
    "X": "xuất file"
  },
  "roles": [
    {
      "key": "platform_admin",
      "brd": "Admin"
    },
    {
      "key": "coop_manager",
      "brd": "Đại diện HTX"
    },
    {
      "key": "htx_member",
      "brd": "Thành viên HTX"
    }
  ],
  "functions": [
    {
      "key": "FN-01",
      "name": "Đăng nhập, đăng xuất",
      "grants": {
        "platform_admin": "R",
        "coop_manager": "R",
        "htx_member": "R"
      }
    },
    {
      "key": "FN-02",
      "name": "Danh mục đơn vị hành chính",
      "grants": {
        "platform_admin": "CRU",
        "coop_manager": "",
        "htx_member": ""
      }
    },
    {
      "key": "FN-03",
      "name": "Quản lý HTX",
      "grants": {
        "platform_admin": "CRUD",
        "coop_manager": "RU",
        "htx_member": "R"
      }
    },
    {
      "key": "FN-04",
      "name": "Quản lý hộ xã viên",
      "grants": {
        "platform_admin": "CRUD",
        "coop_manager": "CRUD",
        "htx_member": "R"
      }
    },
    {
      "key": "FN-07",
      "name": "Quản lý tài khoản của Admin",
      "grants": {
        "platform_admin": "CRUL",
        "coop_manager": "",
        "htx_member": ""
      }
    },
    {
      "key": "FN-08",
      "name": "Quản lý tài khoản của Đại diện HTX",
      "grants": {
        "platform_admin": "",
        "coop_manager": "CRUL",
        "htx_member": ""
      }
    },
    {
      "key": "FN-09",
      "name": "Liên kết và chia sẻ dữ liệu giữa HTX",
      "grants": {
        "platform_admin": "",
        "coop_manager": "CRUD",
        "htx_member": ""
      }
    },
    {
      "key": "FN-10-OTP",
      "name": "Gửi mã OTP đặt lại mật khẩu cho người khác",
      "grants": {
        "platform_admin": "P",
        "coop_manager": "P",
        "htx_member": ""
      }
    },
    {
      "key": "FN-10-SELF",
      "name": "Quên mật khẩu, tự đổi mật khẩu",
      "grants": {
        "platform_admin": "U",
        "coop_manager": "U",
        "htx_member": "U"
      }
    },
    {
      "key": "FN-11",
      "name": "Cấu hình hệ số rơm",
      "grants": {
        "platform_admin": "CRU",
        "coop_manager": "",
        "htx_member": ""
      }
    },
    {
      "key": "FN-12",
      "name": "Thống kê vụ của Thành viên HTX",
      "grants": {
        "platform_admin": "R",
        "coop_manager": "R",
        "htx_member": "R"
      }
    },
    {
      "key": "FN-13",
      "name": "Thống kê vụ của Đại diện HTX",
      "grants": {
        "platform_admin": "R",
        "coop_manager": "R",
        "htx_member": ""
      }
    },
    {
      "key": "FN-14",
      "name": "Thống kê vụ của Admin",
      "grants": {
        "platform_admin": "R",
        "coop_manager": "",
        "htx_member": ""
      }
    },
    {
      "key": "FN-15",
      "name": "Bộ lọc và xuất báo cáo",
      "grants": {
        "platform_admin": "RX",
        "coop_manager": "RX",
        "htx_member": "R"
      }
    },
    {
      "key": "KHMV-FN-01",
      "name": "Danh mục cây trồng, giống và SOP",
      "grants": {
        "platform_admin": "CRUD",
        "coop_manager": "R",
        "htx_member": "R"
      }
    },
    {
      "key": "KHMV-FN-02",
      "name": "Xem danh sách nông hộ và diện tích đất",
      "grants": {
        "platform_admin": "R",
        "coop_manager": "R",
        "htx_member": "R"
      }
    },
    {
      "key": "KHMV-FN-03",
      "name": "Khai báo mở vụ mới (D = dừng/hủy vụ kèm lý do)",
      "grants": {
        "platform_admin": "CRUD",
        "coop_manager": "R",
        "htx_member": "CRUD"
      }
    },
    {
      "key": "KHMV-FN-03-LICH",
      "name": "Lịch thời vụ (lập, sửa, hủy, phát)",
      "grants": {
        "platform_admin": "CRUD",
        "coop_manager": "CRUD",
        "htx_member": "R"
      }
    },
    {
      "key": "KHMV-FN-03-XEM",
      "name": "Xem chi tiết vụ, tiến độ, kế hoạch mùa vụ",
      "grants": {
        "platform_admin": "R",
        "coop_manager": "R",
        "htx_member": "R"
      }
    },
    {
      "key": "KHMV-FN-04",
      "name": "Kế hoạch mùa vụ",
      "grants": {
        "platform_admin": "CRUD",
        "coop_manager": "R",
        "htx_member": "CRUD"
      }
    },
    {
      "key": "KHMV-FN-05",
      "name": "Ghi nhật ký canh tác",
      "grants": {
        "platform_admin": "CRUD",
        "coop_manager": "R",
        "htx_member": "CRUD"
      }
    },
    {
      "key": "KHMV-FN-06",
      "name": "Khai báo sản lượng (D = hủy khai báo)",
      "grants": {
        "platform_admin": "CRUD",
        "coop_manager": "R",
        "htx_member": "CRUD"
      }
    },
    {
      "key": "KHMV-NTF",
      "name": "Thông báo của BRD Kế hoạch mùa vụ",
      "grants": {
        "platform_admin": "",
        "coop_manager": "",
        "htx_member": "R"
      }
    },
    {
      "key": "TTPP-FN-TT-01",
      "name": "Thời tiết hôm nay trên Trang chủ",
      "grants": {
        "platform_admin": "R",
        "coop_manager": "R",
        "htx_member": "R"
      }
    },
    {
      "key": "TTPP-FN-TT-02",
      "name": "Lịch thời tiết (dương lịch và âm lịch)",
      "grants": {
        "platform_admin": "R",
        "coop_manager": "R",
        "htx_member": "R"
      }
    },
    {
      "key": "TTPP-FN-TT-03",
      "name": "Kết nối API thời tiết",
      "grants": {
        "platform_admin": "CRU",
        "coop_manager": "",
        "htx_member": ""
      }
    }
  ],
  "member_write_routes": [
    "POST /v1/notifications/read-all",
    "POST /v1/notifications/:id/read",
    "POST /v1/devices/:id/push-token",
    "POST /v1/cropcycle/seasons",
    "POST /v1/cropcycle/seasons/bulk-open",
    "POST /v1/cropcycle/seasons/:ref/corrections",
    "POST /v1/cropcycle/seasons/:ref/cancel",
    "POST /v1/cropcycle/seasons/:ref/harvest",
    "POST /v1/cropcycle/harvests/bulk",
    "POST /v1/cropcycle/harvests/import",
    "POST /v1/cropcycle/harvests/:id/corrections",
    "POST /v1/cropcycle/farm-logs",
    "POST /v1/cropcycle/farm-logs/batch",
    "POST /v1/cropcycle/farm-logs/:id/corrections",
    "POST /v1/cooperatives/:ref/households/:household_ref/seasons",
    "POST /v1/cooperatives/:ref/households/:household_ref/season-plan-preview",
    "POST /v1/cooperatives/:ref/households/:household_ref/seasons/:season_ref/task-completions",
    "POST /v1/cooperatives/:ref/households/:household_ref/seasons/:season_ref/task-records",
    "POST /v1/cooperatives/:ref/households/:household_ref/seasons/:season_ref/stop",
    "POST /v1/cooperatives/:ref/households/:household_ref/seasons/:season_ref/task-batches",
    "POST /v1/cooperatives/:ref/households/:household_ref/seasons/:season_ref/adhoc-records",
    "POST /v1/cooperatives/:ref/households/:household_ref/seasons/:season_ref/task-records/:log_ref/corrections",
    "POST /v1/cooperatives/:ref/households/:household_ref/seasons/:season_ref/task-records/:log_ref/void",
    "POST /v1/cooperatives/:ref/households/:household_ref/seasons/:season_ref/plan-changes",
    "POST /v1/me/data-consent",
    "PUT /v1/me/password"
  ]
} as const;
