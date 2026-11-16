"use client";

import Link from "next/link";
import { config } from "@/config";
import { BrandMark } from "@/shell/BrandMark";
import { BRAND } from "@/shell/brand";
import { SupportContact } from "@/shell/SupportContact";

/**
 * "Liên hệ quản trị viên" (FN-CH-00.01): yêu cầu hỗ trợ tài khoản. Quên mật khẩu tự đặt lại qua email ở /login/quen-mat-khau
 * (#255). Đầu mối hỗ trợ (tên đơn vị, SĐT, email) lấy từ cấu hình triển khai (FN-01, QTPQ-Q-04); chưa cấu hình thì không hiện.
 */
export default function ContactAdminPage() {
  return (
    <div className="auth-bg">
      <main className="login" style={{ gridTemplateColumns: "minmax(0, 560px)", justifyContent: "center" }}>
        <section className="login-card">
          <div className="brand-row" style={{ color: "var(--slate-900)" }}>
            <BrandMark size={32} />
            {BRAND.productName}
          </div>
          <h1>Liên hệ quản trị viên</h1>
          <p>Cần hỗ trợ tài khoản (chưa có vai trò, phòng ban; tài khoản bị khóa; đổi đơn vị công tác)? Liên hệ quản trị viên hệ thống.</p>
          {config.support ? (
            <>
              <SupportContact info={config.support} />
              <p className="login-note">Khi gọi, cung cấp tên đăng nhập hoặc số điện thoại đăng nhập — không bao giờ đọc mật khẩu cho người khác.</p>
            </>
          ) : (
            <p className="login-note">Chưa có thông tin hỗ trợ được cấu hình. Liên hệ người đã cấp tài khoản cho bạn.</p>
          )}
          <Link className="btn btn-p btn-block" href="/login">
            Quay lại đăng nhập
          </Link>
        </section>
      </main>
    </div>
  );
}
