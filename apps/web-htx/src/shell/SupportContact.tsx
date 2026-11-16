import type { SupportInfo } from "@/config";

/**
 * Thông tin hỗ trợ tĩnh của "Liên hệ quản trị viên" (FN-01, QTPQ-Q-04): tên đơn vị hỗ trợ, số điện thoại, email.
 * Không có biểu mẫu gửi; ba giá trị lấy từ cấu hình triển khai (config.support).
 */
export function SupportContact({ info }: { info: SupportInfo }) {
  return (
    <dl className="support-contact">
      <dt>Đơn vị hỗ trợ</dt>
      <dd>{info.name}</dd>
      <dt>Số điện thoại</dt>
      <dd>
        <a href={`tel:${info.phone.replace(/[^\d+]/g, "")}`}>{info.phone}</a>
      </dd>
      <dt>Email</dt>
      <dd>
        <a href={`mailto:${info.email}`}>{info.email}</a>
      </dd>
    </dl>
  );
}
