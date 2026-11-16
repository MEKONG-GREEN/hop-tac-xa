import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { AlertList, deltaPct, formatDelta, HotMetric, progressColor, sparkPath } from "./dashboard";
import { BreadcrumbTailProvider, useBreadcrumbTailValue } from "@/shell/Breadcrumb";
import { DetailPage, LabelValueBlock, LogPanel, RelatedTabs } from "./detail";
import { useState, type FormEvent } from "react";
import { Collapsible } from "./collapsible";
import { DataTable, FilterBar, FilterChip, pageItems, rangeText, resultCountText, RowActions } from "./list";

const STATUS_OPTIONS = [
  { value: "", label: "Tất cả" },
  { value: "draft", label: "Lưu nháp" },
];

/** Màn gọi mẫu: 2 ô trên hàng, 1 ô trong "Lọc thêm"; tự đếm ô đang đặt và đặt lại trong onClear. */
function FilterHarness() {
  const [a, setA] = useState("draft");
  const [b, setB] = useState("");
  const [c, setC] = useState("");
  const active = [a, b, c].filter(Boolean).length;
  return (
    <FilterBar
      resultCount={3}
      activeCount={active}
      moreActiveCount={c ? 1 : 0}
      onClear={() => {
        setA("");
        setB("");
        setC("");
      }}
      more={<FilterChip label="Loại" value={c} onChange={setC} options={STATUS_OPTIONS} />}
    >
      <FilterChip label="Trạng thái" value={a} onChange={setA} options={STATUS_OPTIONS} />
      <FilterChip label="Vùng" value={b} onChange={setB} options={STATUS_OPTIONS} />
    </FilterBar>
  );
}

describe("List", () => {
  it("số trang: 1 2 3 … 10, lân cận trang hiện tại", () => {
    expect(pageItems(1, 10)).toEqual([1, 2, 3, "gap", 10]);
    expect(pageItems(5, 10)).toEqual([1, "gap", 4, 5, 6, "gap", 10]);
    expect(pageItems(10, 10)).toEqual([1, "gap", 8, 9, 10]);
    expect(pageItems(2, 4)).toEqual([1, 2, 3, 4]);
  });

  it("Hiển thị a–b trên n", () => {
    expect(rangeText(1, 10, 23)).toBe("Hiển thị 1–10 trên 23");
    expect(rangeText(3, 10, 23)).toBe("Hiển thị 21–23 trên 23");
    expect(rangeText(1, 10, 0)).toBe("Không có dòng nào");
  });

  it("bảng có STT theo trang, phân trang bấm được", async () => {
    const user = userEvent.setup();
    const onPage = vi.fn();
    render(
      <DataTable
        caption="Thử"
        columns={[{ key: "n", header: "Tên", cell: (r: { n: string }) => r.n }]}
        rows={[{ n: "a" }, { n: "b" }]}
        rowKey={(r) => r.n}
        page={2}
        pageSize={2}
        total={5}
        onPage={onPage}
      />,
    );
    const rows = screen.getAllByRole("row");
    expect(within(rows[0]!).getByRole("columnheader", { name: "STT" })).toBeInTheDocument();
    expect(within(rows[1]!).getAllByRole("cell")[0]).toHaveTextContent("3");
    expect(screen.getByRole("button", { name: "Trang 2" })).toHaveAttribute("aria-current", "page");
    await user.click(screen.getByRole("button", { name: "Trang sau" }));
    expect(onPage).toHaveBeenCalledWith(3);
  });

  it("bảng không phân trang (thay <table> tự viết) vẫn có STT từ 1, không có thanh trang", () => {
    render(<DataTable caption="Thử" columns={[{ key: "n", header: "Tên", cell: (r: { n: string }) => r.n }]} rows={[{ n: "a" }, { n: "b" }]} rowKey={(r) => r.n} />);
    const rows = screen.getAllByRole("row");
    expect(within(rows[2]!).getAllByRole("cell")[0]).toHaveTextContent("2");
    expect(screen.queryByRole("navigation")).toBeNull();
  });

  it("UX-01: index={false} bỏ cột STT; dòng rỗng trải đủ số cột thực có", () => {
    render(<DataTable caption="Thử" index={false} columns={[{ key: "n", header: "Tên", cell: (r: { n: string }) => r.n }]} rows={[]} rowKey={(r) => r.n} />);
    expect(screen.queryByRole("columnheader", { name: "STT" })).toBeNull();
    expect(screen.getAllByRole("columnheader")).toHaveLength(1);
    expect(screen.getByRole("cell")).toHaveAttribute("colspan", "1");
  });

  it("AC-N3: thanh lọc có số kết quả; Xóa lọc vô hiệu khi chưa đặt ô nào, bấm thì mọi ô về mặc định", async () => {
    const user = userEvent.setup();
    render(<FilterHarness />);
    expect(screen.getByText("3 kết quả")).toBeInTheDocument();
    const clear = screen.getByRole("button", { name: "Xóa lọc" });
    expect(clear).toBeEnabled();
    expect(screen.getByRole("combobox", { name: "Trạng thái" }).closest(".fchip")).toHaveClass("is-set");
    expect(screen.queryByRole("button", { name: "Đặt lại" })).toBeNull();

    // ô trong phần ẩn: nút "Lọc thêm" báo số ô đang đặt, mặc định đóng
    const more = screen.getByRole("button", { name: "Lọc thêm" });
    expect(more).toHaveAttribute("aria-expanded", "false");
    expect(screen.getByLabelText("Lọc thêm", { selector: "div" })).not.toBeVisible();
    await user.click(more);
    expect(more).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByRole("group", { name: "Lọc thêm" })).toBeVisible();
    await user.selectOptions(screen.getByRole("combobox", { name: "Loại" }), "draft");
    expect(screen.getByRole("button", { name: "Lọc thêm (1)" })).toBeInTheDocument();

    await user.click(clear);
    for (const name of ["Trạng thái", "Vùng", "Loại"]) expect(screen.getByRole("combobox", { name })).toHaveValue("");
    expect(document.querySelector(".fchip.is-set")).toBeNull();
    expect(screen.getByRole("button", { name: "Xóa lọc" })).toBeDisabled();
  });

  it("số kết quả theo vi-VN; không có phần ẩn thì không có nút Lọc thêm", () => {
    expect(resultCountText(1234)).toBe("1.234 kết quả");
    render(<FilterBar resultCount={0} activeCount={0} onClear={() => undefined} />);
    expect(screen.getByText("0 kết quả")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Lọc thêm/ })).toBeNull();
  });

  it("AC-N2: nút chính, nút phụ, menu Thêm chứa các thao tác còn lại", async () => {
    const user = userEvent.setup();
    const onCopy = vi.fn();
    const onDelete = vi.fn();
    render(
      <RowActions
        label="HTX A"
        primary={{ label: "Sửa", href: "/x?sua=1" }}
        secondary={{ label: "Xem", href: "/x" }}
        more={[{ label: "Sao chép", onClick: onCopy }, { label: "Xóa", onClick: onDelete, danger: true }]}
      />,
    );
    expect(screen.getByRole("link", { name: "Sửa HTX A" })).toHaveClass("btn-p");
    expect(screen.getByRole("link", { name: "Xem HTX A" })).toHaveClass("btn-o");
    expect(screen.queryByRole("button", { name: "Xóa HTX A" })).toBeNull();
    expect(screen.queryByRole("menu")).toBeNull();

    const trigger = screen.getByRole("button", { name: "Thêm thao tác cho HTX A" });
    expect(trigger).toHaveTextContent("Thêm");
    await user.click(trigger);
    const items = within(screen.getByRole("menu")).getAllByRole("menuitem");
    expect(items.map((i) => i.textContent)).toEqual(["Sao chép", "Xóa"]);
    expect(items[1]).toHaveClass("danger");

    await user.keyboard("{Escape}");
    expect(screen.queryByRole("menu")).toBeNull();
    expect(trigger).toHaveFocus();

    await user.click(trigger);
    await user.click(screen.getByRole("menuitem", { name: "Xóa" }));
    expect(onDelete).toHaveBeenCalledTimes(1);
    expect(onCopy).not.toHaveBeenCalled();
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("menu Thêm đóng khi bấm ra ngoài", async () => {
    const user = userEvent.setup();
    render(<RowActions label="HTX A" primary={{ label: "Sửa", href: "/x" }} more={[{ label: "Ẩn", onClick: () => undefined }]} />);
    await user.click(screen.getByRole("button", { name: "Thêm thao tác cho HTX A" }));
    expect(screen.getByRole("menu")).toBeInTheDocument();
    await user.click(document.body);
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("chỉ có quyền xem: nút chính là Xem, không có nút phụ và không có nút Thêm", () => {
    render(<RowActions label="HTX A" primary={{ label: "Xem", href: "/x" }} />);
    expect(screen.getByRole("link", { name: "Xem HTX A" })).toHaveClass("btn-p");
    expect(screen.queryByRole("link", { name: "Sửa HTX A" })).toBeNull();
    expect(screen.queryByRole("button", { name: /Thêm thao tác/ })).toBeNull();
    // `more` rỗng cũng không hiện nút Thêm
    render(<RowActions label="HTX B" primary={{ label: "Xem", href: "/y" }} more={[]} />);
    expect(screen.queryByRole("button", { name: /Thêm thao tác/ })).toBeNull();
  });

  it("kiểu chặn xóa/hủy làm nút chính hoặc nút phụ", () => {
    // Kiểm bằng `tsc --noEmit`: dòng @ts-expect-error sai (không còn lỗi kiểu) sẽ làm tsc đỏ.
    const noop = () => undefined;
    const elements = [
      // @ts-expect-error `danger` chỉ có ở thao tác trong menu "Thêm"
      <RowActions key="p" label="A" primary={{ label: "Xóa", onClick: noop, danger: true }} />,
      // @ts-expect-error `danger` chỉ có ở thao tác trong menu "Thêm"
      <RowActions key="s" label="A" primary={{ label: "Sửa", onClick: noop }} secondary={{ label: "Hủy", onClick: noop, danger: true }} />,
    ];
    expect(elements).toHaveLength(2);
  });
});

describe("Collapsible", () => {
  it("AC3: mặc định đóng, bấm thì mở, bấm lại thì đóng; nội dung vẫn nằm trong DOM", async () => {
    const user = userEvent.setup();
    render(
      <Collapsible title="Thông tin thêm">
        <input aria-label="Ghi chú" />
      </Collapsible>,
    );
    const head = screen.getByRole("button", { name: "Thông tin thêm" });
    expect(head).toHaveAttribute("aria-expanded", "false");
    expect(screen.getByLabelText("Ghi chú", { selector: "input" })).not.toBeVisible();
    await user.click(head);
    expect(head).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByRole("region", { name: "Thông tin thêm" })).toBeVisible();
    await user.type(screen.getByLabelText("Ghi chú"), "abc");
    await user.click(head);
    expect(screen.getByLabelText("Ghi chú", { selector: "input" })).not.toBeVisible();
    expect(screen.getByLabelText("Ghi chú", { selector: "input", exact: true })).toHaveValue("abc");
  });

  it("gửi form khi ô bắt buộc trong khối đóng còn trống: form bị chặn và khối tự mở để thấy ô lỗi", async () => {
    const user = userEvent.setup();
    const onSubmit = vi.fn((e: FormEvent) => e.preventDefault());
    render(
      <form onSubmit={onSubmit}>
        <Collapsible title="Thông tin thêm">
          <input aria-label="Ghi chú" required />
        </Collapsible>
        <button type="submit">Lưu</button>
      </form>,
    );
    const head = screen.getByRole("button", { name: "Thông tin thêm" });
    expect(head).toHaveAttribute("aria-expanded", "false");
    await user.click(screen.getByRole("button", { name: "Lưu" }));
    expect(onSubmit).not.toHaveBeenCalled();
    expect(head).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByLabelText("Ghi chú")).toBeVisible();
    expect(screen.getByLabelText("Ghi chú")).toBeInvalid();
  });
});

describe("Detail", () => {
  it("DetailPage (R-A2): ← quay lại, tiêu đề h1, nút góc phải, mắt xích breadcrumb; không có Log thì một khối", () => {
    function Tail() {
      return <output aria-label="tail">{useBreadcrumbTailValue().map((c) => c.label).join(" / ")}</output>;
    }
    render(
      <BreadcrumbTailProvider>
        <Tail />
        <DetailPage back={{ href: "/kn/du-lieu/htx", label: "Danh sách HTX" }} title="Thêm HTX" actions={<button type="button">Lưu</button>}>
          <p>Nội dung form</p>
        </DetailPage>
      </BreadcrumbTailProvider>,
    );
    expect(screen.getByRole("link", { name: "Danh sách HTX" })).toHaveAttribute("href", "/kn/du-lieu/htx");
    expect(screen.getByRole("heading", { level: 1, name: "Thêm HTX" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Lưu" })).toBeInTheDocument();
    expect(screen.getByRole("status", { name: "tail" })).toHaveTextContent("Thêm HTX");
    expect(document.querySelector(".detail-grid")).toBeNull();
  });

  it("khối Nhãn – Giá trị, giá trị trống → —", () => {
    render(<LabelValueBlock title="Nội dung 1" items={[{ label: "Tên", value: "HTX A" }, { label: "Ghi chú", value: "" }]} />);
    expect(screen.getByRole("heading", { name: "Nội dung 1" })).toBeInTheDocument();
    expect(screen.getByText("Ghi chú").nextSibling).toHaveTextContent("—");
  });

  it("khung Log: sự kiện, người · thời điểm, cũ → mới, lý do", () => {
    render(
      <LogPanel
        entries={[{ event: "Cập nhật diện tích", actor_display: "Nguyễn Văn An", at: "2026-09-07T03:20:00Z", changes: [{ field: "Diện tích", old: "118", new: "120" }], reason: "Đo lại" }]}
      />,
    );
    const log = screen.getByRole("complementary", { name: "Log" });
    expect(log).toHaveTextContent("Cập nhật diện tích");
    expect(log).toHaveTextContent(/Nguyễn Văn An · .*07\/09\/2026/);
    expect(log).toHaveTextContent("Diện tích: 118 → 120");
    expect(log).toHaveTextContent("Lý do: Đo lại");
  });

  it("tab liên quan", async () => {
    const user = userEvent.setup();
    render(
      <RelatedTabs
        tabs={[
          { id: "a", label: "Liên quan 1", content: <p>Nội dung A</p> },
          { id: "b", label: "Liên quan 2", content: <p>Nội dung B</p> },
        ]}
      />,
    );
    expect(screen.getByText("Nội dung A")).toBeInTheDocument();
    await user.click(screen.getByRole("tab", { name: "Liên quan 2" }));
    expect(screen.getByRole("tab", { name: "Liên quan 2" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByText("Nội dung B")).toBeInTheDocument();
  });
});

describe("Dashboard", () => {
  it("sparkline, delta, màu tiến độ theo ngưỡng", () => {
    expect(sparkPath([1]).line).toBe("");
    expect(sparkPath([0, 10]).line).toBe("M0.0 36.0 L100.0 8.0");
    expect(formatDelta(deltaPct(106.2, 100))).toBe("+6,2%");
    expect(formatDelta(deltaPct(95.9, 100))).toBe("−4,1%");
    expect(progressColor(90)).toBe("var(--primary)");
    expect(progressColor(35)).toBe("var(--chart-orange)");
    expect(progressColor(60)).toBe("var(--chart-blue)");
  });

  it("thẻ Chỉ số nóng và danh sách Cảnh báo theo mức", () => {
    render(
      <>
        <HotMetric label="DT xuống giống lúa (ha)" value="72.410" delta="+6,2% so kỳ trước" series={[1, 2, 3]} />
        <AlertList
          alerts={[
            { id: "1", level: "urgent", title: "Rầy nâu tăng mạnh", at: "2026-09-07T01:15:00Z", href: "/x" },
            { id: "2", level: "info", title: "Đã cập nhật tổng hợp", at: "2026-09-08T02:15:00Z" },
          ]}
          allHref="/thong-bao"
        />
      </>,
    );
    expect(screen.getByText("72.410")).toBeInTheDocument();
    const alerts = screen.getByRole("region", { name: "Cảnh báo" });
    expect(within(alerts).getByText("Khẩn")).toBeInTheDocument();
    expect(within(alerts).getByText("Thông tin")).toBeInTheDocument();
    expect(within(alerts).getByText("2")).toHaveClass("count-pill");
    expect(within(alerts).getByRole("link", { name: "Xem: Rầy nâu tăng mạnh" })).toHaveAttribute("href", "/x");
    expect(within(alerts).getByRole("link", { name: "Xem tất cả" })).toHaveAttribute("href", "/thong-bao");
  });
});
