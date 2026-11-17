// SINH TỪ contracts/htxroles/htx-roles.v1.json — không sửa tay; chạy `npm run gen:api` trong apps/web-htx.
// Ma trận quyền Mục V (FN-05): docs/conventions/vai-tro-app-htx.md.
export const HTX_ROLE_MATRIX = {
  "version": 1,
  "source": "BRD_App_HTX_QuanTriPhanQuyen_HoSoHTX_ThongKeBaoCao_v3.0 — Mục V (FN-05, ADM-01); quyết định nhóm dự án 17/11/2026 (AH-272)",
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
    }
  ],
  "member_write_routes": [
    "POST /v1/notifications/read-all",
    "POST /v1/notifications/:id/read",
    "POST /v1/devices/:id/push-token"
  ]
} as const;
