import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { normalizeVi } from "@/cgh/pins";
import { FilterBar, FilterChip, nextSort, RowActions, SearchBox, type SortState } from "./list";
import { clientList, compareValues, ListScreen, type ClientTab } from "./listScreen";

interface Row {
  code: string;
  name: string;
  phone: string;
  hamlet: string;
  active: boolean;
  kind: "a" | "b";
}

const NAMES = ["Đặng Văn An", "Bùi Thị Bé", "Ân Minh", "Lê Cường", "Phạm Dung", "Hồ Đông", "Trần Em", "Võ Phúc"];
const ROWS: Row[] = Array.from({ length: 23 }, (_, i) => ({
  code: `M${String(i + 1).padStart(2, "0")}`,
  name: `${NAMES[i % NAMES.length]} ${i + 1}`,
  phone: `09000000${String(i + 1).padStart(2, "0")}`,
  hamlet: `Ấp ${(i % 4) + 1}`,
  active: i % 3 !== 0,
  kind: i % 2 === 0 ? "a" : "b",
}));
const TABS: ClientTab<Row>[] = [
  { id: "all", label: "Tất cả", match: () => true },
  { id: "active", label: "Đang hoạt động", match: (r) => r.active },
  { id: "off", label: "Ngừng", match: (r) => !r.active },
];
const KIND_OPTIONS = [
  { value: "", label: "Tất cả" },
  { value: "a", label: "Loại A" },
];
const sortValue = (r: Row, k: string) => (k === "code" ? r.code : k === "name" ? r.name : undefined);

function Harness({ onOpen = () => {}, onEdit = () => {} }: { onOpen?: (r: Row) => void; onEdit?: (r: Row) => void }) {
  const [tab, setTab] = useState("all");
  const [q, setQ] = useState("");
  const [kind, setKind] = useState("");
  const [sort, setSort] = useState<SortState | null>(null);
  const [page, setPage] = useState(1);
  const term = normalizeVi(q);
  const list = clientList(ROWS, {
    tabs: TABS,
    tab,
    filter: (r) => (!term || normalizeVi(`${r.code} ${r.name}`).includes(term)) && (!kind || r.kind === kind),
    sort,
    sortValue,
    page,
  });
  const reset = <V,>(set: (v: V) => void) => (v: V) => {
    set(v);
    setPage(1);
  };
  return (
    <ListScreen
      caption="Danh sách thử"
      tabsLabel="Trạng thái"
      tabs={list.tabs}
      tab={tab}
      onTab={reset(setTab)}
      filterBar={
        <FilterBar
          resultCount={list.total}
          activeCount={(q.trim() ? 1 : 0) + (kind ? 1 : 0)}
          onClear={() => {
            setQ("");
            setKind("");
            setPage(1);
          }}
        >
          <SearchBox value={q} onChange={reset(setQ)} placeholder="Tìm theo mã hoặc tên" />
          <FilterChip label="Loại" value={kind} options={KIND_OPTIONS} onChange={reset(setKind)} />
        </FilterBar>
      }
      code={{ key: "code", header: "Mã", cell: (r) => r.code }}
      main={{ key: "name", header: "Tên", sortKey: "name", title: (r) => r.name, sub: (r) => r.phone, open: (r) => ({ onClick: () => onOpen(r) }) }}
      columns={[{ key: "hamlet", header: "Ấp", cell: (r) => r.hamlet }]}
      actions={{
        cell: (r) => <RowActions label={r.name} primary={{ label: "Sửa", onClick: () => onEdit(r) }} more={[{ label: "Xóa", danger: true, onClick: () => {} }]} />,
      }}
      rows={list.rows}
      rowKey={(r) => r.code}
      total={list.total}
      page={list.page}
      onPage={setPage}
      sort={sort}
      onSort={reset(setSort)}
    />
  );
}

const tabCount = (name: string) => Number(within(screen.getByRole("tab", { name: new RegExp(`^${name}`) })).getByText(/^\d+$/).textContent);
const dataRows = () => screen.getAllByRole("row").slice(1);
const firstCol = () => dataRows().map((r) => within(r).getAllByRole("cell")[0]!.textContent);

describe("Mẫu danh sách UX-01 (AH-292)", () => {
  it("AC-N1: đủ thành phần UX-01", () => {
    render(<Harness />);
    expect(screen.getByRole("tablist", { name: "Trạng thái" })).toBeInTheDocument();
    expect(tabCount("Tất cả")).toBe(23);
    expect(screen.getByRole("searchbox", { name: "Tìm theo mã hoặc tên" })).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Loại" })).toBeInTheDocument();
    const heads = screen.getAllByRole("columnheader");
    expect(heads[0]).toHaveTextContent("Mã");
    expect(screen.queryByRole("columnheader", { name: "STT" })).toBeNull();
    expect(screen.getByRole("button", { name: /^Tên/ })).toBeInTheDocument();
    expect(screen.getByText("Hiển thị 1–10 trên 23")).toBeInTheDocument();
    expect(dataRows()).toHaveLength(10);
    const first = dataRows()[0]!;
    expect(within(first).getByRole("button", { name: ROWS[0]!.name })).toBeInTheDocument();
    expect(within(first).getByText(ROWS[0]!.phone)).toBeInTheDocument();
    expect(within(first).getByRole("button", { name: `Sửa ${ROWS[0]!.name}` })).toBeInTheDocument();
  });

  it("AC-N1: số trên tab khớp số dòng sau khi tìm kiếm và lọc", async () => {
    const user = userEvent.setup();
    render(<Harness />);
    await user.type(screen.getByRole("searchbox"), "dang");
    await user.selectOptions(screen.getByRole("combobox", { name: "Loại" }), "a");
    const expected = ROWS.filter((r) => normalizeVi(`${r.code} ${r.name}`).includes("dang") && r.kind === "a");
    expect(expected.length).toBeGreaterThan(0);
    for (const t of TABS) {
      await user.click(screen.getByRole("tab", { name: new RegExp(`^${t.label}`) }));
      const n = expected.filter(t.match).length;
      expect(tabCount(t.label)).toBe(n);
      expect(screen.getByText(`${n} kết quả`)).toBeInTheDocument();
      if (n > 0) expect(screen.getByText(new RegExp(`trên ${n}$`))).toBeInTheDocument();
      expect(dataRows()).toHaveLength(n === 0 ? 1 : Math.min(n, 10));
    }
    await user.click(screen.getByRole("button", { name: "Xóa lọc" }));
    expect(tabCount("Tất cả")).toBe(23);
    expect(tabCount("Ngừng")).toBe(ROWS.filter((r) => !r.active).length);
  });

  it("sắp xếp theo cột: aria-sort, đảo chiều, về trang 1", async () => {
    const user = userEvent.setup();
    render(<Harness />);
    await user.click(screen.getByRole("button", { name: "Trang 2" }));
    const th = screen.getByRole("columnheader", { name: /^Tên/ });
    expect(th).not.toHaveAttribute("aria-sort");
    await user.click(within(th).getByRole("button"));
    expect(th).toHaveAttribute("aria-sort", "ascending");
    expect(screen.getByText("Hiển thị 1–10 trên 23")).toBeInTheDocument();
    const names = [...ROWS].map((r) => r.name).sort((a, b) => compareValues(a, b));
    const shown = () => dataRows().map((r) => within(r).getAllByRole("cell")[1]!.querySelector(".lst-main")!.textContent);
    expect(shown()).toEqual(names.slice(0, 10));
    await user.click(within(th).getByRole("button"));
    expect(th).toHaveAttribute("aria-sort", "descending");
    expect(shown()).toEqual([...names].reverse().slice(0, 10));
    expect(screen.getByRole("columnheader", { name: "Mã" })).not.toHaveAttribute("aria-sort");
  });

  it("phân trang 10 dòng; đổi tab về trang 1", async () => {
    const user = userEvent.setup();
    render(<Harness />);
    await user.click(screen.getByRole("button", { name: "Trang 3" }));
    expect(screen.getByText("Hiển thị 21–23 trên 23")).toBeInTheDocument();
    expect(dataRows()).toHaveLength(3);
    expect(firstCol()).toEqual(["M21", "M22", "M23"]);
    await user.click(screen.getByRole("tab", { name: /^Đang hoạt động/ }));
    expect(screen.getByText(/^Hiển thị 1–10 trên/)).toBeInTheDocument();
  });

  it("bấm dòng mở chi tiết; bàn phím Enter trên tên cũng mở", async () => {
    const user = userEvent.setup();
    const onOpen = vi.fn();
    render(<Harness onOpen={onOpen} />);
    await user.click(screen.getByText("M05"));
    expect(onOpen).toHaveBeenCalledTimes(1);
    expect(onOpen).toHaveBeenLastCalledWith(ROWS[4]);
    onOpen.mockClear();
    screen.getByRole("button", { name: ROWS[1]!.name }).focus();
    await user.keyboard("{Enter}");
    expect(onOpen).toHaveBeenCalledTimes(1);
    expect(onOpen).toHaveBeenLastCalledWith(ROWS[1]);
  });

  it("bấm nút cuối dòng không mở dòng", async () => {
    const user = userEvent.setup();
    const onOpen = vi.fn();
    const onEdit = vi.fn();
    render(<Harness onOpen={onOpen} onEdit={onEdit} />);
    await user.click(screen.getByRole("button", { name: `Sửa ${ROWS[2]!.name}` }));
    expect(onEdit).toHaveBeenCalledWith(ROWS[2]);
    expect(onOpen).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: `Thêm thao tác cho ${ROWS[2]!.name}` }));
    await user.click(screen.getByRole("menuitem", { name: "Xóa" }));
    expect(onOpen).not.toHaveBeenCalled();
  });
});

describe("clientList, compareValues, nextSort", () => {
  const base = { tabs: TABS, tab: "all", filter: () => true, sort: null, sortValue };

  it("kẹp trang vượt số trang; tab lạ lấy tất cả", () => {
    const r = clientList(ROWS, { ...base, page: 99 });
    expect(r.page).toBe(3);
    expect(r.rows).toHaveLength(3);
    expect(clientList(ROWS, { ...base, tab: "khong-co", page: 1 }).total).toBe(23);
    expect(clientList(ROWS, { ...base, page: 0 }).page).toBe(1);
  });

  it("giá trị rỗng xuống cuối ở cả hai chiều; sắp ổn định", () => {
    const rows = [
      { code: "b", v: 2 },
      { code: "x", v: undefined },
      { code: "a", v: 2 },
      { code: "c", v: 1 },
    ];
    const run = (dir: "asc" | "desc") =>
      clientList(rows, { tabs: [], tab: "", filter: () => true, sort: { key: "v", dir }, sortValue: (r) => r.v, page: 1 }).rows.map((r) => r.code);
    expect(run("asc")).toEqual(["c", "b", "a", "x"]);
    expect(run("desc")).toEqual(["b", "a", "c", "x"]);
  });

  it("compareValues: số so số, chuỗi so theo tiếng Việt và số trong chuỗi", () => {
    expect(compareValues(2, 10)).toBeLessThan(0);
    expect(compareValues("M2", "M10")).toBeLessThan(0);
    expect(compareValues("Ân", "Bình")).toBeLessThan(0);
    expect(compareValues("", "a")).toBeGreaterThan(0);
    expect(compareValues(null, null)).toBe(0);
  });

  it("nextSort: cột mới tăng dần, cùng cột đảo chiều", () => {
    expect(nextSort(null, "a")).toEqual({ key: "a", dir: "asc" });
    expect(nextSort({ key: "a", dir: "asc" }, "a")).toEqual({ key: "a", dir: "desc" });
    expect(nextSort({ key: "a", dir: "desc" }, "b")).toEqual({ key: "b", dir: "asc" });
  });
});
