import { describe, expect, it } from "vitest";
import {
  ACTIVITIES,
  ACTIVITY_LABEL,
  addDays,
  blankMaterial,
  blankTask,
  flatIndex,
  fromSopVersion,
  harvestRowError,
  locationLabel,
  MSG_HARVEST_STEP,
  MSG_SOWING_WINDOW,
  moveItem,
  normalizePhone,
  parseDecimalInput,
  previewYield,
  reasonError,
  sowingDateError,
  stageHasRange,
  toCsv,
  todayVN,
  toSopPayload,
  validateSop,
  type SopDraft,
  type SopMaterialDraft,
  type SopStageDraft,
  type SopTaskDraft,
} from "./rules";

const task = (name: string, patch: Partial<SopTaskDraft> = {}): SopTaskDraft => ({
  ...blankTask(),
  name,
  window: { kind: "date_range", anchor: "nxg", from: "0", to: "3" },
  ...patch,
});
const stage = (name: string, tasks: SopTaskDraft[], from = "", to = ""): SopStageDraft => ({ name, from, to, tasks });
const harvestTask = (patch: Partial<SopTaskDraft> = {}) => task("Thu hoạch", { activity: "thu_hoach", window: { kind: "date_range", anchor: "nxg", from: "95", to: "100" }, ...patch });
const sop = (stages: SopStageDraft[], patch: Partial<SopDraft> = {}): SopDraft => ({
  code: "",
  name: "SOP lúa",
  crop: "LUA",
  varietyCode: "",
  anchorName: "Ngày xuống giống",
  stages,
  ...patch,
});
const paths = (d: SopDraft, forPublish = false) => validateSop(d, forPublish).map((i) => i.path);

describe("soạn SOP — kiểm bản nháp (AH-8 AC2)", () => {
  it("bản nháp hợp lệ: không lỗi", () => {
    expect(validateSop(sop([stage("Chuẩn bị", [task("Làm đất")], "-3", "1"), stage("Thu hoạch", [harvestTask()])]), true)).toEqual([]);
  });

  it("thông tin chung bắt buộc: tên, loại cây, tên mốc gốc", () => {
    expect(paths(sop([stage("A", [task("x")])], { name: " ", crop: "", anchorName: "" })).sort()).toEqual(["anchor_name", "crop", "name"]);
  });

  it("giai đoạn: tên bắt buộc và không trùng, Từ–Đến cả hai, Từ ≤ Đến, cho phép số âm", () => {
    const d = sop([
      stage("Làm đất", [], "-5", "2"),
      stage("  làm   ĐẤT ", [], "9", "2"),
      stage("", [], "3", ""),
      stage("Một nửa", [], "", "4"),
    ]);
    expect(paths(d).sort()).toEqual(["stages[1].name", "stages[1].to_day", "stages[2].name", "stages[2].to_day", "stages[3].from_day"].sort());
  });

  it("việc: tên bắt buộc, không trùng trong cùng giai đoạn nhưng được trùng ở giai đoạn khác; tối đa một việc tổng kết vụ", () => {
    const d = sop([stage("A", [task("Bón"), task(" bón "), task("")]), stage("B", [task("Bón", { seasonSummary: true }), task("Khác", { seasonSummary: true })])]);
    expect(paths(d).sort()).toEqual(["steps[1].name", "steps[2].name", "steps[4].season_summary"]);
  });

  it("chỉ số steps[j] đếm liên tục qua các giai đoạn", () => {
    const stages = [stage("A", [task("1"), task("2")]), stage("B", [task("3")])];
    expect([flatIndex(stages, 0, 1), flatIndex(stages, 1, 0)]).toEqual([1, 2]);
    const bad = sop([stage("A", [task("1"), task("2")]), stage("B", [task("3", { window: { kind: "date_range", anchor: "nxg", from: "5", to: "1" } })])]);
    expect(paths(bad)).toEqual(["steps[2].window.to"]);
  });

  it("vật tư: liều > 0, tối đa 4 chữ số lẻ, đơn vị bắt buộc, không lặp, chọn danh mục hoặc nhập tên", () => {
    const m = (patch: Partial<SopMaterialDraft>): SopMaterialDraft => ({ ...blankMaterial(), unit: "kg", dose: "1", ...patch });
    const d = sop([
      stage("A", [
        task("Bón", {
          materials: [
            m({ materialCode: "URE", name: "Urê", dose: "0" }),
            m({ materialCode: "URE", name: "Urê" }),
            m({ name: "Tự pha", dose: "-1" }),
            m({ name: "Tự pha 2", dose: "1,12345" }),
            m({ name: "Tự pha 3", unit: "" }),
            m({ name: "" }),
          ],
        }),
      ]),
    ]);
    expect(paths(d).sort()).toEqual(
      [
        "steps[0].materials[0].dose_per_ha",
        "steps[0].materials[1].material_code",
        "steps[0].materials[2].dose_per_ha",
        "steps[0].materials[3].dose_per_ha",
        "steps[0].materials[4].unit",
        "steps[0].materials[5].name",
      ].sort(),
    );
  });
});

describe("soạn SOP — bốn loại cửa sổ (AH-8 AC3)", () => {
  const win = (patch: Partial<SopTaskDraft["window"]>) => sop([stage("A", [task("x", { window: { kind: "date_range", anchor: "nxg", from: "0", to: "1", ...patch } })], "0", "10")]);

  it("Theo ngày: cả ba mốc, số âm hợp lệ, Từ ≤ Đến", () => {
    for (const anchor of ["nxg", "nth", "ntt"] as const) expect(paths(win({ anchor, from: "-10", to: "-4" }))).toEqual([]);
    expect(paths(win({ from: "4", to: "3" }))).toEqual(["steps[0].window.to"]);
    expect(paths(win({ from: "", to: "" })).sort()).toEqual(["steps[0].window.from", "steps[0].window.to"]);
    expect(paths(win({ from: "2.5" }))).toEqual(["steps[0].window.from"]);
  });

  it("Mở: có mốc và Từ, không cần Đến", () => {
    expect(paths(win({ kind: "open", from: "7", to: "" }))).toEqual([]);
    expect(paths(win({ kind: "open", from: "" }))).toEqual(["steps[0].window.from"]);
  });

  it("Theo giai đoạn: chỉ hợp lệ khi giai đoạn đã có khoảng ngày; Không ngày luôn hợp lệ", () => {
    expect(paths(win({ kind: "stage" }))).toEqual([]);
    const noRange = sop([stage("A", [task("x", { window: { kind: "stage", anchor: "nxg", from: "", to: "" } })])]);
    expect(paths(noRange)).toEqual(["steps[0].window.kind"]);
    expect(paths(win({ kind: "none", from: "", to: "" }))).toEqual([]);
    expect(stageHasRange({ from: "0", to: "10" })).toBe(true);
    expect(stageHasRange({ from: "0", to: "" })).toBe(false);
  });
});

describe("soạn SOP — chặn phát hành (AH-8 AC5)", () => {
  it("SOP rỗng, giai đoạn chưa có việc, không có việc Thu hoạch", () => {
    expect(paths(sop([]), true)).toEqual(["stages"]);
    expect(paths(sop([stage("A", [harvestTask()]), stage("B", [])]), true)).toEqual(["stages[1]"]);
    expect(paths(sop([stage("A", [task("x")])]), true)).toEqual(["steps"]);
    expect(validateSop(sop([stage("A", [task("x")])]), true)[0]?.message).toBe(MSG_HARVEST_STEP);
    expect(paths(sop([stage("A", [task("x")])]), false)).toEqual([]);
  });

  it("việc Thu hoạch cuối có cửa sổ Mở, Không ngày, mốc NTH hoặc NTT thì chặn; Theo ngày NXG và Theo giai đoạn thì qua", () => {
    const withWin = (window: SopTaskDraft["window"]) => sop([stage("A", [harvestTask({ window })], "90", "110")]);
    const blocked = [
      { kind: "open", anchor: "nxg", from: "95", to: "" },
      { kind: "none", anchor: "nxg", from: "", to: "" },
      { kind: "date_range", anchor: "nth", from: "0", to: "1" },
      { kind: "date_range", anchor: "ntt", from: "0", to: "1" },
    ] as const;
    for (const w of blocked) expect(paths(withWin({ ...w }), true)).toEqual(["steps[0].window"]);
    expect(paths(withWin({ kind: "date_range", anchor: "nxg", from: "95", to: "100" }), true)).toEqual([]);
    expect(paths(withWin({ kind: "stage", anchor: "nxg", from: "", to: "" }), true)).toEqual([]);
  });

  it("việc dùng NTH nhưng Thu hoạch cuối không có cửa sổ ngày → chặn (đúng câu chữ AC5)", () => {
    const d = sop([stage("A", [task("Kiểm tra", { window: { kind: "date_range", anchor: "nth", from: "-5", to: "-1" } }), harvestTask({ window: { kind: "none", anchor: "nxg", from: "", to: "" } })])]);
    expect(paths(d, true)).toEqual(["steps[1].window"]);
  });

  it("Thu hoạch cuối là việc thu_hoach có thứ tự lớn nhất", () => {
    const d = sop([stage("A", [harvestTask({ window: { kind: "none", anchor: "nxg", from: "", to: "" } }), task("x"), harvestTask({ name: "Thu hoạch đợt cuối" })])]);
    expect(paths(d, true)).toEqual([]);
  });
});

describe("soạn SOP — thân gửi máy chủ và đọc lại", () => {
  const draft = sop([
    stage("Chuẩn bị", [task("Làm đất", { activity: "lam_dat", window: { kind: "date_range", anchor: "nxg", from: "-3", to: "-1" } })], "-3", "1"),
    stage(
      "Chăm sóc",
      [
        task("Bón thúc", {
          activity: "bon_phan",
          window: { kind: "open", anchor: "nth", from: "-20", to: "" },
          seasonSummary: true,
          phiDays: "14",
          materials: [
            { materialCode: "URE", name: "Urê", dose: "50,50", unit: "kg" },
            { materialCode: "", name: " Phân tự pha ", dose: "1", unit: "lít" },
          ],
        }),
        task("Kiểm tra", { activity: "kiem_tra", window: { kind: "stage", anchor: "nxg", from: "", to: "" } }),
      ],
      "2",
      "90",
    ),
  ]);

  it("toSopPayload đánh seq toàn cục, stage_seq, task_seq và đổi số về dạng máy chủ nhận", () => {
    const p = toSopPayload(draft);
    expect(p.stages).toEqual([
      { seq: 1, name: "Chuẩn bị", from_day: -3, to_day: 1 },
      { seq: 2, name: "Chăm sóc", from_day: 2, to_day: 90 },
    ]);
    expect(p.steps.map((s) => [s.seq, s.stage_seq, s.task_seq])).toEqual([[1, 1, 1], [2, 2, 1], [3, 2, 2]]);
    expect(p.steps[1]).toMatchObject({
      window: { kind: "open", anchor: "nth", from: -20 },
      season_summary: true,
      phi_days: 14,
      materials: [
        { material_code: "URE", dose_per_ha: "50.5", unit: "kg" },
        { name: "Phân tự pha", dose_per_ha: "1", unit: "lít" },
      ],
    });
    expect(p.steps[2]?.window).toEqual({ kind: "stage" });
    expect(p.steps[1]?.window).not.toHaveProperty("to");
  });

  it("fromSopVersion đọc lại đúng nhóm giai đoạn, cửa sổ và vật tư", () => {
    const p = toSopPayload(draft);
    const version = { stages: p.stages, steps: p.steps.map((s) => ({ ...s, day_offset: 0, window_days: 0, mandatory: s.mandatory ?? false })) } as unknown as Parameters<typeof fromSopVersion>[1];
    const back = fromSopVersion({ code: "SOP-A", name: "SOP lúa", crop: "LUA", variety_code: null, anchor_name: "Ngày xuống giống" }, version);
    expect(back.stages.map((s) => [s.name, s.from, s.to, s.tasks.length])).toEqual([["Chuẩn bị", "-3", "1", 1], ["Chăm sóc", "2", "90", 2]]);
    expect(back.stages[1]?.tasks[0]).toMatchObject({ seasonSummary: true, phiDays: "14", window: { kind: "open", anchor: "nth", from: "-20" } });
    expect(back.stages[1]?.tasks[0]?.materials).toHaveLength(2);
    expect(toSopPayload(back).steps.map((s) => s.name)).toEqual(["Làm đất", "Bón thúc", "Kiểm tra"]);
  });

  it("đọc rồi gửi lại một phiên bản không làm mất instruction, evidence_kinds, stage_code", () => {
    const p = toSopPayload(draft);
    const steps = p.steps.map((s, i) => ({
      ...s,
      day_offset: 0,
      window_days: 0,
      mandatory: s.mandatory ?? false,
      ...(i === 1 ? { instruction: "Bón khi lúa đẻ nhánh", evidence_kinds: ["anh_hien_truong", "hoa_don_vat_tu"], stage_code: "de_nhanh" } : {}),
    }));
    const version = { stages: p.stages, steps } as unknown as Parameters<typeof fromSopVersion>[1];
    const back = fromSopVersion({ code: "SOP-A", name: "SOP lúa", crop: "LUA", variety_code: null, anchor_name: "Ngày xuống giống" }, version);
    const again = toSopPayload(back).steps;
    expect(again[1]).toMatchObject({ instruction: "Bón khi lúa đẻ nhánh", evidence_kinds: ["anh_hien_truong", "hoa_don_vat_tu"], stage_code: "de_nhanh" });
    expect(again[0]).not.toHaveProperty("instruction");
    expect(again[0]).not.toHaveProperty("evidence_kinds");
    expect(again[0]).not.toHaveProperty("stage_code");
  });

  it("moveItem đổi chỗ, ngoài biên giữ nguyên", () => {
    expect(moveItem(["a", "b", "c"], 2, -1)).toEqual(["a", "c", "b"]);
    expect(moveItem(["a", "b"], 0, -1)).toEqual(["a", "b"]);
    expect(moveItem(["a", "b"], 1, 1)).toEqual(["a", "b"]);
  });

  it("nhãn loại việc theo BRD, đủ 9 loại", () => {
    expect(ACTIVITIES).toHaveLength(9);
    expect(ACTIVITY_LABEL.gieo_sa).toBe("Gieo trồng");
    expect(ACTIVITY_LABEL.phun_thuoc).toBe("Phun thuốc - dưỡng");
    expect(ACTIVITY_LABEL.cat_tia).toBe("Cắt tỉa");
    expect(ACTIVITY_LABEL.khac).toBe("Khác");
  });
});

describe("lý do sửa/hủy (RB-1)", () => {
  it("tối thiểu 10 ký tự sau khi bỏ khoảng trắng", () => {
    expect(reasonError("   ")).toBe("Bắt buộc nhập lý do.");
    expect(reasonError("  ngắn  ")).toMatch(/tối thiểu 10/);
    expect(reasonError("Nhập nhầm giống lúa")).toBeNull();
    expect(reasonError("12345678901234567890", 20)).toBeNull();
  });
});

describe("SĐT chủ hộ (HTX US-ACC-01)", () => {
  it("chuẩn hoá về 0xxxxxxxxx", () => {
    expect(normalizePhone("0999 012 345")).toBe("0999012345");
    expect(normalizePhone("+84999012345")).toBe("0999012345");
    expect(normalizePhone("84999012345")).toBe("0999012345");
    expect(normalizePhone("0999.012.345")).toBe("0999012345");
    expect(normalizePhone("099901234")).toBeNull();
    expect(normalizePhone("abc")).toBeNull();
  });
});

describe("số thập phân nhập tay (R-10)", () => {
  it("dấu phẩy thập phân, bỏ số 0 thừa, giới hạn chữ số lẻ", () => {
    expect(parseDecimalInput("6.250,5".replace(".", ""))).toBe("6250.5");
    expect(parseDecimalInput("0012,500")).toBe("12.5");
    expect(parseDecimalInput("7")).toBe("7");
    expect(parseDecimalInput("1.2345")).toBeNull();
    expect(parseDecimalInput("-3")).toBeNull();
    expect(parseDecimalInput("-3", 3, true)).toBe("-3");
    expect(parseDecimalInput("1e3")).toBeNull();
  });
});

describe("ngày sạ (HTX US-SEASON-01 AC-2)", () => {
  it("trong [hôm nay − 15, hôm nay + 30]", () => {
    const today = "2026-09-29";
    expect(sowingDateError(today, today)).toBeNull();
    expect(sowingDateError(addDays(today, -15), today)).toBeNull();
    expect(sowingDateError(addDays(today, 30), today)).toBeNull();
    expect(sowingDateError(addDays(today, -16), today)).toBe(MSG_SOWING_WINDOW);
    expect(sowingDateError(addDays(today, 31), today)).toBe(MSG_SOWING_WINDOW);
    expect(sowingDateError("2026-02-30", today)).toBe("Ngày không hợp lệ.");
  });

  it("ngày nghiệp vụ theo giờ Việt Nam", () => {
    // 29/09/2026 18:30 UTC = 30/09/2026 01:30 giờ Việt Nam.
    expect(todayVN(new Date("2026-09-29T18:30:00Z"))).toBe("2026-09-30");
    expect(addDays("2026-12-31", 1)).toBe("2027-01-01");
  });
});

describe("bảng nhập nhanh sản lượng (HTX US-YIELD-02/03)", () => {
  const today = "2026-09-29";
  it("thiếu/sai số/âm/ngày tương lai bị báo; 0 kg hợp lệ (backend yêu cầu xác nhận bất thường)", () => {
    expect(harvestRowError({ mass: "", date: today }, today)).toMatch(/Nhập sản lượng/);
    expect(harvestRowError({ mass: "abc", date: today }, today)).toMatch(/phải là số/);
    expect(harvestRowError({ mass: "-5", date: today }, today)).toBe("Sản lượng không được âm.");
    expect(harvestRowError({ mass: "6500", date: "2026-10-01" }, today)).toMatch(/tương lai/);
    expect(harvestRowError({ mass: "0", date: today }, today)).toBeNull();
    expect(harvestRowError({ mass: "6500,5", date: "" }, today)).toBeNull();
  });

  it("năng suất xem trước t/ha", () => {
    expect(previewYield("6500", "1.0000")).toBe("6.50");
    expect(previewYield("6500", "0")).toBeNull();
  });
});

describe("nhãn vị trí (HTX US-LOG-05 AC-1)", () => {
  it("mismatch/missing có nhãn cảnh báo đúng chữ", () => {
    expect(locationLabel("mismatch").text).toBe("Vị trí không khớp");
    expect(locationLabel("missing").text).toBe("Thiếu GPS");
    expect(locationLabel("match").cls).toBe("c-ok");
  });
});

describe("xuất CSV (HTX US-DASH-02, xuất phía client)", () => {
  it("BOM, thoát dấu phẩy/nháy, chặn công thức", () => {
    const csv = toCsv([
      ["Giống", "Ghi chú"],
      ["OM5451", 'có "nháy", phẩy'],
      ["=HYPERLINK(1)", "-12.5"],
    ]);
    expect(csv.startsWith("﻿")).toBe(true);
    expect(csv).toContain('"có ""nháy"", phẩy"');
    expect(csv).toContain("'=HYPERLINK(1)");
    expect(csv).toContain(",-12.5\r\n");
  });
});
