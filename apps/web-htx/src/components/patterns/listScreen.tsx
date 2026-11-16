"use client";

// Mẫu màn danh sách UX-01 (AH-292, BRD UX-01): tab trạng thái/phân loại kèm số lượng; ô tìm kiếm và bộ lọc; cột đầu là mã;
// ô chính gồm tên + dòng phụ; sắp xếp theo cột; phân trang 10 dòng; bấm dòng để mở chi tiết/sửa; nút thao tác cuối dòng.
// Chỉ ghép các mẫu sẵn có (`Tabs`, `FilterBar`, `DataTable`). Thành phần không giữ dữ liệu: màn gọi giữ tab, tìm kiếm,
// sắp xếp, trang và đưa vào dòng của trang hiện tại. Danh sách tải đủ ở trình duyệt dùng `clientList` để đếm tab, lọc, sắp
// và cắt trang; danh sách phân trang ở máy chủ truyền số đếm và trang của máy chủ thẳng vào đây.
import type { ReactNode } from "react";
import { Tabs, type TabDef } from "../Tabs";
import { DataTable, MainCell, type Column, type SortState } from "./list";

export const LIST_PAGE_SIZE = 10;

export interface ListMainColumn<T> {
  key: string;
  header: string;
  sortKey?: string;
  title: (row: T) => ReactNode;
  sub?: (row: T) => ReactNode;
  /** Bấm tên hoặc bấm dòng: mở chi tiết (`href`) hoặc màn sửa (`onClick`). */
  open: (row: T) => { href: string } | { onClick: () => void };
}

export function ListScreen<T>({
  caption,
  tabsLabel,
  tabs,
  tab,
  onTab,
  filterBar,
  code,
  main,
  columns = [],
  actions,
  rows,
  rowKey,
  total,
  page,
  onPage,
  sort,
  onSort,
  empty,
}: {
  caption: string;
  tabsLabel: string;
  /** Tab kèm số lượng (`count`). */
  tabs: TabDef[];
  tab: string;
  onTab: (id: string) => void;
  /** Thanh lọc do màn gọi ghép (`FilterBar` + `SearchBox` + `FilterChip`). */
  filterBar: ReactNode;
  /** Cột đầu: mã. */
  code: Column<T>;
  main: ListMainColumn<T>;
  columns?: Column<T>[];
  actions: { header?: string; cell: (row: T) => ReactNode };
  /** Dòng của trang hiện tại. */
  rows: T[];
  rowKey: (row: T) => string;
  /** Tổng số dòng sau tìm kiếm, bộ lọc và tab. */
  total: number;
  page: number;
  onPage: (p: number) => void;
  sort: SortState | null;
  onSort: (s: SortState) => void;
  empty?: ReactNode;
}) {
  const all: Column<T>[] = [
    { ...code, className: `lst-code${code.className ? ` ${code.className}` : ""}` },
    {
      key: main.key,
      header: main.header,
      sortKey: main.sortKey,
      cell: (row) => <MainCell title={main.title(row)} sub={main.sub?.(row)} open={main.open(row)} />,
    },
    ...columns,
    {
      key: "actions",
      header: actions.header ?? "Thao tác",
      cell: (row) => (
        <div data-row-actions="" className="lst-acts">
          {actions.cell(row)}
        </div>
      ),
    },
  ];
  return (
    <Tabs tabs={tabs} value={tab} onChange={onTab} label={tabsLabel}>
      {filterBar}
      <DataTable
        caption={caption}
        columns={all}
        rows={rows}
        rowKey={rowKey}
        page={page}
        pageSize={LIST_PAGE_SIZE}
        total={total}
        onPage={onPage}
        index={false}
        sort={sort}
        onSort={onSort}
        openRowOnClick
        empty={empty}
      />
    </Tabs>
  );
}

export interface ClientTab<T> {
  id: string;
  label: string;
  match: (row: T) => boolean;
}

const isBlank = (v: unknown) => v === null || v === undefined || v === "";

/** So hai giá trị sắp xếp: rỗng luôn xuống cuối, số so số, chuỗi so theo tiếng Việt (số trong chuỗi so theo giá trị). */
export function compareValues(a: string | number | null | undefined, b: string | number | null | undefined): number {
  if (isBlank(a) || isBlank(b)) return Number(isBlank(a)) - Number(isBlank(b));
  if (typeof a === "number" && typeof b === "number") return a - b;
  return String(a).localeCompare(String(b), "vi", { numeric: true, sensitivity: "base" });
}

/**
 * Danh sách tải đủ ở trình duyệt: lọc → đếm từng tab trên tập đã lọc → lấy dòng của tab đang chọn → sắp (ổn định) → cắt trang.
 * Nhờ thứ tự này số trên tab đang chọn luôn bằng `total` và bằng số kết quả của thanh lọc (AC-N1).
 */
export function clientList<T>(
  rows: readonly T[],
  o: {
    tabs: ClientTab<T>[];
    tab: string;
    /** Tìm kiếm và bộ lọc của màn gọi. */
    filter: (row: T) => boolean;
    sort: SortState | null;
    sortValue: (row: T, key: string) => string | number | null | undefined;
    page: number;
    pageSize?: number;
  },
): { tabs: TabDef[]; rows: T[]; total: number; page: number } {
  const pageSize = o.pageSize ?? LIST_PAGE_SIZE;
  const filtered = rows.filter(o.filter);
  const tabs: TabDef[] = o.tabs.map((t) => ({ id: t.id, label: t.label, count: filtered.filter(t.match).length }));
  const current = o.tabs.find((t) => t.id === o.tab);
  const inTab = current ? filtered.filter(current.match) : filtered;
  const { sort } = o;
  const sorted = sort
    ? inTab
        .map((row, i) => ({ row, i }))
        .sort((x, y) => {
          const c = compareValues(o.sortValue(x.row, sort.key), o.sortValue(y.row, sort.key));
          // Rỗng luôn xuống cuối ở cả hai chiều: chỉ đảo chiều khi cả hai đều có giá trị.
          const blank = isBlank(o.sortValue(x.row, sort.key)) || isBlank(o.sortValue(y.row, sort.key));
          return (blank || sort.dir === "asc" ? c : -c) || x.i - y.i;
        })
        .map((x) => x.row)
    : inTab;
  const pages = Math.max(1, Math.ceil(sorted.length / pageSize));
  const page = Math.min(Math.max(1, o.page), pages);
  return { tabs, rows: sorted.slice((page - 1) * pageSize, page * pageSize), total: sorted.length, page };
}
