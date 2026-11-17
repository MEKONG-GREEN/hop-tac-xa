"use client";

// Thông báo phạm vi dữ liệu của App HTX (FN-06, docs/conventions/vai-tro-app-htx.md): rời trang ngoài phạm vi kèm câu
// ngắn ở trang đầu tiên được xem (EX-01, AC-N2) và trạng thái rỗng cho tài khoản chưa gắn HTX (EX-02, AC-N1).
// Đặt ở khung chung (AppShell) — không lặp điều kiện ở từng màn. Máy chủ vẫn kiểm mọi yêu cầu (TECH-01).
import { createContext, useContext, useEffect } from "react";
import type { ApiProblem } from "@/api/problem";
import { Empty } from "@/components/ui";
import { useStoredValue, writeStoredValue } from "@/lib/useStoredValue";

/** Một câu cho cả "không tồn tại" và "ngoài phạm vi" — tách ra là lộ điều máy chủ đang giấu (404). */
export const OUT_OF_SCOPE_NOTICE = "Không tìm thấy dữ liệu, hoặc bạn không có quyền xem dữ liệu này.";
export const NO_COOP_NOTICE = "Tài khoản chưa được gắn HTX. Liên hệ người cấp tài khoản.";

export const NOTICE_KEY = "ag.shell.notice";

/** Thông báo khi rời trang: hiện ở `to`, xoá khi người dùng sang trang khác cả `from` lẫn `to`. */
export interface ShellNotice {
  from: string;
  to: string;
  text: string;
}

export function writeShellNotice(n: ShellNotice): void {
  writeStoredValue(NOTICE_KEY, JSON.stringify(n), "session");
}

function parseNotice(raw: string | null | undefined): ShellNotice | null {
  if (!raw) return null;
  try {
    const v: unknown = JSON.parse(raw);
    if (typeof v !== "object" || v === null) return null;
    const { from, to, text } = v as Record<string, unknown>;
    return typeof from === "string" && typeof to === "string" && typeof text === "string" ? { from, to, text } : null;
  } catch {
    return null;
  }
}

/** Câu thông báo cần hiện ở trang hiện tại (null khi không có). */
export function useShellNotice(pathname: string): string | null {
  const notice = parseNotice(useStoredValue(NOTICE_KEY, "session"));
  const stale = !!notice && pathname !== notice.from && pathname !== notice.to;
  useEffect(() => {
    if (stale) writeStoredValue(NOTICE_KEY, null, "session");
  }, [stale]);
  return notice && pathname === notice.to ? notice.text : null;
}

/**
 * Rời trang hiện tại về trang đầu tiên được xem kèm câu thông báo. AppShell chỉ cung cấp cho người dùng App HTX;
 * vai khác (khuyến nông…) và ngoài khung nhận null.
 */
export const ShellExitContext = createContext<((text: string) => void) | null>(null);

/** Người dùng là Đại diện HTX / Thành viên HTX chưa gắn HTX (lacksCoop — AppShell cung cấp; ngoài khung là false). */
export const ShellNoCoopContext = createContext(false);

/**
 * Màn chi tiết của App HTX nhận 404 (không tồn tại hoặc ngoài phạm vi, FN-06/EX-01) → rời về trang đầu tiên được xem.
 * Trả true khi đang rời (màn hiện Loading thay cho lỗi). Vai ngoài App HTX vẫn thấy lỗi như cũ.
 */
export function useExitOnNotFound(problem: ApiProblem | null | undefined): boolean {
  const exit = useContext(ShellExitContext);
  const leaving = !!exit && problem?.status === 404;
  useEffect(() => {
    if (leaving && exit) exit(OUT_OF_SCOPE_NOTICE);
  }, [leaving, exit]);
  return leaving;
}

/** Lỗi "tài khoản chưa gắn đơn vị" của tài khoản App HTX chưa gắn HTX → hiện NoCoopState thay cho lỗi chung (AC-N1). */
export function useIsNoCoopProblem(problem: Pick<ApiProblem, "code">): boolean {
  return useContext(ShellNoCoopContext) && problem.code === "node_scope_required";
}

/** Trạng thái rỗng cho tài khoản chưa gắn HTX (AC-N1). */
export function NoCoopState() {
  return (
    <div className="card sec" role="status">
      <Empty>{NO_COOP_NOTICE}</Empty>
    </div>
  );
}
