import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { SeasonPlan, type SeasonPlanView } from "./SeasonPlan";

describe("AH-71: lịch dự kiến theo giai đoạn", () => {
  it("phân biệt Không ngày, Mở, khoảng ngày và chưa có mốc; chỉ đọc", () => {
    const plan: SeasonPlanView = { as_of_date: "2026-11-18", stages: [{ seq: 1, name: "Chăm sóc", steps: [
      { seq: 1, name: "Theo dõi", from_date: null, to_date: null, undated: true, status: "due" },
      { seq: 2, name: "Tưới", from_date: "2026-11-01", to_date: null, undated: false, status: "due" },
      { seq: 3, name: "Sau thu hoạch", from_date: null, to_date: null, undated: false, status: "waiting_anchor" },
      { seq: 4, name: "Bón phân", from_date: "2026-11-01", to_date: "2026-11-02", undated: false, status: "overdue" },
    ] }] };
    const { rerender } = render(<SeasonPlan plan={plan} />);
    expect(screen.getByRole("heading", { name: "1. Chăm sóc" })).toBeTruthy();
    expect(screen.getByText("Chưa đặt ngày")).toBeTruthy();
    const open = screen.getByRole("row", { name: /Tưới/ });
    expect(within(open).getByText("Đến hạn")).toBeTruthy();
    expect(within(open).getByText(/Từ/)).toBeTruthy();
    expect(screen.getByText("Chờ mốc")).toBeTruthy();
    expect(screen.queryByRole("button")).toBeNull();
    rerender(<SeasonPlan plan={{ ...plan, stages: [] }} />);
    expect(screen.getByText("Chưa có việc trong kế hoạch.")).toBeTruthy();
    expect(screen.queryByText("Theo dõi")).toBeNull();
  });
});
