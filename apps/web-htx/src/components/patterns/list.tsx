"use client";

// Mẫu trang List (BRD §4.0 mục 3, Design Sample "Screens / List"): tiêu đề + nút, thanh bộ lọc dạng thẻ
// một hàng có số kết quả, "Lọc thêm" và "Xóa lọc", bảng có STT, nhãn trạng thái, hàng nút mỗi dòng (nút chính,
// tối đa một nút phụ, còn lại trong menu "Thêm"), phân trang số trang. Bảng còn có chế độ UX-01 (AH-292): bỏ cột STT,
// sắp xếp theo cột, bấm dòng để mở. Quy ước: docs/conventions/giao-dien-chung.md.
import Link from "next/link";
import { useEffect, useId, useRef, useState, type MouseEvent as ReactMouseEvent, type ReactNode } from "react";
import { Icon } from "../Icon";

export interface FilterOption {
  value: string;
  label: string;
}

/** Thẻ lọc "Nhãn  Giá trị ▾" (select gốc phủ trong suốt → bàn phím, trình đọc màn hình dùng được). */
export function FilterChip({
  label,
  value,
  options,
  onChange,
  defaultValue = "",
}: {
  label: string;
  value: string;
  options: FilterOption[];
  onChange: (v: string) => void;
  /** Giá trị "Tất cả"; khác giá trị này → thẻ tô xanh (đã lọc). */
  defaultValue?: string;
}) {
  const current = options.find((o) => o.value === value)?.label ?? value;
  return (
    <span className={`fchip${value !== defaultValue ? " is-set" : ""}`}>
      <span aria-hidden="true">{label}</span>
      <b aria-hidden="true">{current}</b>
      <Icon name="chevronDown" size={14} />
      <select aria-label={label} value={value} onChange={(e) => onChange(e.target.value)}>
        {options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
    </span>
  );
}

export function SearchBox({ value, onChange, placeholder = "Tìm kiếm" }: { value: string; onChange: (v: string) => void; placeholder?: string }) {
  return (
    <label className="fsearch">
      <Icon name="search" size={16} />
      <span className="sr-only">{placeholder}</span>
      <input type="search" value={value} placeholder={placeholder} onChange={(e) => onChange(e.target.value)} />
    </label>
  );
}

export function resultCountText(n: number): string {
  return `${n.toLocaleString("vi-VN")} kết quả`;
}

/**
 * Thanh lọc một hàng: ô lọc (`children`, quy ước tối đa 4), "Lọc thêm" (các ô trong `more`), số kết quả, "Xóa lọc".
 * Màn gọi tự đếm số ô đang đặt và đặt lại giá trị trong `onClear`; thành phần không đo độ rộng.
 */
export function FilterBar({
  children,
  more,
  resultCount,
  activeCount,
  moreActiveCount = 0,
  onClear,
}: {
  children?: ReactNode;
  more?: ReactNode;
  resultCount: number;
  /** Số ô lọc đang đặt (cả hàng và phần ẩn); 0 → "Xóa lọc" bị vô hiệu. */
  activeCount: number;
  /** Số ô đang đặt trong phần "Lọc thêm". */
  moreActiveCount?: number;
  onClear: () => void;
}) {
  const [moreOpen, setMoreOpen] = useState(false);
  const moreId = useId();
  return (
    <div className="card filters" role="search" aria-label="Bộ lọc">
      <div className="filters-row">
        {children}
        {more ? (
          <button type="button" className="fmore" aria-expanded={moreOpen} aria-controls={moreId} onClick={() => setMoreOpen((v) => !v)}>
            <Icon name="filter" size={15} />
            {moreActiveCount > 0 ? `Lọc thêm (${moreActiveCount})` : "Lọc thêm"}
          </button>
        ) : null}
        <span className="fcount" aria-live="polite">
          {resultCountText(resultCount)}
        </span>
        <button type="button" className="freset" disabled={activeCount === 0} onClick={onClear}>
          <Icon name="refresh" size={15} />
          Xóa lọc
        </button>
      </div>
      {more ? (
        <div id={moreId} className="filters-more" role="group" aria-label="Lọc thêm" hidden={!moreOpen}>
          {more}
        </div>
      ) : null}
    </div>
  );
}

export type PillTone = "neutral" | "ok" | "warn" | "bad" | "info";

export function StatusPill({ tone = "neutral", children }: { tone?: PillTone; children: ReactNode }) {
  return <span className={`pill${tone === "neutral" ? "" : ` pill-${tone}`}`}>{children}</span>;
}

export type RowAction = { label: string } & ({ href: string; onClick?: never } | { onClick: () => void; href?: never });
export type MoreAction = RowAction & { danger?: boolean };

/**
 * Hàng nút của một dòng: một nút chính, tối đa một nút phụ, còn lại trong menu "Thêm" (ẩn khi rỗng).
 * Xóa/hủy chỉ đặt trong `more`: `primary`/`secondary` không có trường `danger`.
 * `label` là tên dòng cho nhãn trợ năng: "Sửa HTX Mỹ Hòa".
 */
export function RowActions({ label, primary, secondary, more }: { label: string; primary: RowAction; secondary?: RowAction; more?: MoreAction[] }) {
  const [open, setOpen] = useState(false);
  const wrap = useRef<HTMLSpanElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const id = useId();

  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => {
      if (wrap.current && !wrap.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      setOpen(false);
      trigger.current?.focus();
    };
    document.addEventListener("mousedown", onDoc);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDoc);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const button = (a: RowAction, cls: string) =>
    a.href !== undefined ? (
      <Link href={a.href} className={cls} aria-label={`${a.label} ${label}`}>
        {a.label}
      </Link>
    ) : (
      <button type="button" className={cls} aria-label={`${a.label} ${label}`} onClick={a.onClick}>
        {a.label}
      </button>
    );

  return (
    <span className="row-acts" ref={wrap}>
      {button(primary, "btn btn-p btn-sm")}
      {secondary ? button(secondary, "btn btn-o btn-sm") : null}
      {more && more.length > 0 ? (
        <span className="row-more">
          <button
            type="button"
            ref={trigger}
            className="btn btn-o btn-sm"
            aria-haspopup="menu"
            aria-expanded={open}
            aria-controls={`${id}-menu`}
            aria-label={`Thêm thao tác cho ${label}`}
            onClick={() => setOpen((v) => !v)}
          >
            <Icon name="more" size={16} />
            Thêm
          </button>
          {open ? (
            <div className="row-more-menu" role="menu" id={`${id}-menu`} aria-label={`Thao tác thêm cho ${label}`}>
              {more.map((m) => {
                const cls = `row-more-item${m.danger ? " danger" : ""}`;
                return m.href !== undefined ? (
                  <Link key={m.label} href={m.href} role="menuitem" className={cls} onClick={() => setOpen(false)}>
                    {m.label}
                  </Link>
                ) : (
                  <button
                    key={m.label}
                    type="button"
                    role="menuitem"
                    className={cls}
                    onClick={() => {
                      setOpen(false);
                      m.onClick();
                    }}
                  >
                    {m.label}
                  </button>
                );
              })}
            </div>
          ) : null}
        </span>
      ) : null}
    </span>
  );
}

/** Số trang hiển thị: 1 2 3 … 10 (luôn có trang đầu, cuối, lân cận trang hiện tại). */
export function pageItems(current: number, total: number): (number | "gap")[] {
  if (total <= 7) return Array.from({ length: total }, (_, i) => i + 1);
  const set = new Set<number>([1, total, current - 1, current, current + 1]);
  if (current <= 3) [2, 3].forEach((n) => set.add(n));
  if (current >= total - 2) [total - 1, total - 2].forEach((n) => set.add(n));
  const nums = [...set].filter((n) => n >= 1 && n <= total).sort((a, b) => a - b);
  const out: (number | "gap")[] = [];
  nums.forEach((n, i) => {
    if (i > 0 && n - nums[i - 1]! > 1) out.push("gap");
    out.push(n);
  });
  return out;
}

export function rangeText(page: number, pageSize: number, total: number): string {
  if (total === 0) return "Không có dòng nào";
  const from = (page - 1) * pageSize + 1;
  const to = Math.min(page * pageSize, total);
  return `Hiển thị ${from}–${to} trên ${total.toLocaleString("vi-VN")}`;
}

export function Pagination({ page, pageSize, total, onPage }: { page: number; pageSize: number; total: number; onPage: (p: number) => void }) {
  const pages = Math.max(1, Math.ceil(total / pageSize));
  return (
    <div className="pager">
      <span aria-live="polite">{rangeText(page, pageSize, total)}</span>
      <nav aria-label="Phân trang">
        <ol>
          <li>
            <button type="button" aria-label="Trang trước" disabled={page <= 1} onClick={() => onPage(page - 1)}>
              ‹
            </button>
          </li>
          {pageItems(page, pages).map((p, i) =>
            p === "gap" ? (
              <li key={`g${i}`} className="gap" aria-hidden="true">
                …
              </li>
            ) : (
              <li key={p}>
                <button type="button" aria-current={p === page ? "page" : undefined} aria-label={`Trang ${p}`} onClick={() => onPage(p)}>
                  {p}
                </button>
              </li>
            ),
          )}
          <li>
            <button type="button" aria-label="Trang sau" disabled={page >= pages} onClick={() => onPage(page + 1)}>
              ›
            </button>
          </li>
        </ol>
      </nav>
    </div>
  );
}

export type SortDir = "asc" | "desc";
export interface SortState {
  key: string;
  dir: SortDir;
}

/** Bấm cột khác → tăng dần; bấm lại cột đang sắp → đảo chiều. */
export function nextSort(current: SortState | null, key: string): SortState {
  if (current && current.key === key) return { key, dir: current.dir === "asc" ? "desc" : "asc" };
  return { key, dir: "asc" };
}

export interface Column<T> {
  key: string;
  header: string;
  cell: (row: T) => ReactNode;
  className?: string;
  /** Có giá trị → bấm tiêu đề cột để sắp xếp (cần `onSort` của bảng). */
  sortKey?: string;
}

/** Phần tử bên trong dòng tự xử lý bấm: bấm vào đó không kích hoạt việc mở dòng. */
const ROW_INTERACTIVE = "a, button, input, select, textarea, label, [role='menu'], [role='menuitem'], [data-row-actions]";

function openFromRow(e: ReactMouseEvent<HTMLTableRowElement>) {
  if ((e.target as Element).closest(ROW_INTERACTIVE)) return;
  if (window.getSelection()?.toString()) return; // đang bôi chữ để sao chép
  e.currentTarget.querySelector<HTMLElement>("[data-row-open]")?.click();
}

/**
 * Ô chính của dòng (UX-01): tên và một dòng phụ (SĐT, ấp, HTX…). Tên là liên kết hoặc nút thật (`data-row-open`):
 * bàn phím dùng được và bấm vào dòng sẽ bấm hộ phần tử này (xem `DataTable` `openRowOnClick`).
 */
export function MainCell({ title, sub, open }: { title: ReactNode; sub?: ReactNode; open: { href: string } | { onClick: () => void } }) {
  return (
    <>
      {"href" in open ? (
        <Link data-row-open="" className="lst-main" href={open.href}>
          {title}
        </Link>
      ) : (
        <button type="button" data-row-open="" className="lst-main" onClick={open.onClick}>
          {title}
        </button>
      )}
      {sub ? <div className="lst-sub">{sub}</div> : null}
    </>
  );
}

/**
 * Bảng danh sách có cột STT do hệ thống đánh (R-A5, theo trang) và phân trang số trang.
 * Không truyền `onPage` → bảng không phân trang (STT từ 1), dùng thay `<table className="dt">` tự viết.
 * `index={false}` bỏ cột STT (mẫu UX-01: cột đầu là mã). `sort`/`onSort` + `Column.sortKey`: tiêu đề cột là nút sắp xếp,
 * bảng chỉ báo trạng thái lên, màn gọi tự sắp. `openRowOnClick`: bấm dòng bấm hộ `[data-row-open]` (xem `MainCell`).
 */
export function DataTable<T>({
  caption,
  columns,
  rows,
  rowKey,
  page = 1,
  pageSize = rows.length,
  total = rows.length,
  onPage,
  empty = "Không có dữ liệu phù hợp bộ lọc.",
  index = true,
  sort,
  onSort,
  openRowOnClick = false,
}: {
  caption: string;
  columns: Column<T>[];
  rows: T[];
  rowKey: (row: T) => string;
  page?: number;
  pageSize?: number;
  total?: number;
  onPage?: (p: number) => void;
  empty?: ReactNode;
  index?: boolean;
  sort?: SortState | null;
  onSort?: (s: SortState) => void;
  openRowOnClick?: boolean;
}) {
  const sortIcon = (dir: SortDir | null) => (dir === "asc" ? "▲" : dir === "desc" ? "▼" : "↕");
  return (
    <div className="card dt-card">
      <div className="tbl-wrap">
        <table className="dt">
          <caption className="sr-only">{caption}</caption>
          <thead>
            <tr>
              {index ? <th scope="col">STT</th> : null}
              {columns.map((c) => {
                const sortKey = onSort ? c.sortKey : undefined;
                const dir = sortKey !== undefined && sort?.key === sortKey ? sort.dir : null;
                return (
                  <th
                    key={c.key}
                    scope="col"
                    className={c.className}
                    aria-sort={dir === "asc" ? "ascending" : dir === "desc" ? "descending" : undefined}
                  >
                    {sortKey !== undefined && onSort ? (
                      <button type="button" className="th-sort" onClick={() => onSort(nextSort(sort ?? null, sortKey))}>
                        {c.header}
                        <span aria-hidden="true" className="th-sort-ic">
                          {sortIcon(dir)}
                        </span>
                      </button>
                    ) : (
                      c.header
                    )}
                  </th>
                );
              })}
            </tr>
          </thead>
          <tbody>
            {rows.length ? (
              rows.map((r, i) => (
                <tr key={rowKey(r)} className={openRowOnClick ? "is-openable" : undefined} onClick={openRowOnClick ? openFromRow : undefined}>
                  {index ? <td className="stt">{(page - 1) * pageSize + i + 1}</td> : null}
                  {columns.map((c) => (
                    <td key={c.key} className={c.className}>
                      {c.cell(r)}
                    </td>
                  ))}
                </tr>
              ))
            ) : (
              <tr>
                <td colSpan={columns.length + (index ? 1 : 0)} className="empty">
                  {empty}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      {onPage ? <Pagination page={page} pageSize={pageSize} total={total} onPage={onPage} /> : null}
    </div>
  );
}

/** Tiêu đề trang + nút thao tác bên phải (nút chính xanh). */
export function PageHeader({ title, badge, actions, children }: { title: ReactNode; badge?: ReactNode; actions?: ReactNode; children?: ReactNode }) {
  return (
    <div className="pg-head">
      <div>
        <h1>
          {title} {badge}
        </h1>
        {children ? <p>{children}</p> : null}
      </div>
      {actions ? <div className="row">{actions}</div> : null}
    </div>
  );
}
