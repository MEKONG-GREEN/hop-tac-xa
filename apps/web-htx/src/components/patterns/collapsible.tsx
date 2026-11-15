"use client";

// Khối thu/mở dùng chung (AC3): luôn đóng khi hiện lần đầu, không có tham số mở sẵn.
// Nội dung giữ trong DOM (ẩn bằng `hidden`) để không mất giá trị đang nhập trong form.
// Ô trong khối đóng không hợp lệ khi gửi form thì khối tự mở, để trình duyệt focus được ô đó
// và hiện thông báo lỗi; nếu không, form bị chặn mà người dùng không thấy lý do.
import { useId, useState, type ReactNode } from "react";
import { flushSync } from "react-dom";
import { Icon } from "../Icon";

export function Collapsible({ title, children }: { title: string; children: ReactNode }) {
  const [open, setOpen] = useState(false);
  const id = useId();
  // Mở đồng bộ: trình duyệt focus ô lỗi ngay sau sự kiện `invalid`, trước lượt render kế tiếp.
  const openForInvalid = () => {
    if (!open) flushSync(() => setOpen(true));
  };
  return (
    <section className="collapsible">
      <button type="button" id={`${id}-head`} className="collapsible-head" aria-expanded={open} aria-controls={`${id}-body`} onClick={() => setOpen((v) => !v)}>
        <Icon name={open ? "chevronDown" : "chevronRight"} size={16} />
        {title}
      </button>
      <div id={`${id}-body`} className="collapsible-body" role="region" aria-labelledby={`${id}-head`} hidden={!open} onInvalidCapture={openForInvalid}>
        {children}
      </div>
    </section>
  );
}
