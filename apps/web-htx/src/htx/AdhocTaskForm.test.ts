import { describe, expect, it } from "vitest";
import { adhocErrors } from "./AdhocTaskForm";

const ok = { name: "Rải vôi bổ sung", activity: "bon_phan", stage: "1", doneOn: "2026-11-20", materials: [{ name: "Vôi bột", unit: "kg", dose: "50" }], backfill: "" };

describe("AH-163 adhocErrors", () => {
  it("accepts a complete adhoc task", () => {
    expect(adhocErrors(ok, "2026-11-21")).toEqual([]);
  });
  it("requires content, activity, stage and a date not after today", () => {
    const errs = adhocErrors({ ...ok, name: " ", activity: "", stage: "", doneOn: "2026-11-22" }, "2026-11-21");
    expect(errs).toEqual(["Nhập nội dung việc.", "Chọn loại việc.", "Chọn giai đoạn.", "Ngày làm không được sau hôm nay."]);
  });
  it("rejects negative or over-precise doses and asks a reason for backfill beyond 3 days", () => {
    expect(adhocErrors({ ...ok, materials: [{ name: "Vôi", unit: "kg", dose: "-1" }] }, "2026-11-21")).toEqual(["Vật tư 1: liều không âm, tối đa 4 chữ số thập phân."]);
    expect(adhocErrors({ ...ok, doneOn: "2026-11-17" }, "2026-11-21")).toEqual(["Nhập lý do khi ghi bù quá 3 ngày."]);
    expect(adhocErrors({ ...ok, doneOn: "2026-11-17", backfill: "Quên ghi" }, "2026-11-21")).toEqual([]);
  });
});
