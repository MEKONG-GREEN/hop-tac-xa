// Quy tắc kiểm tra phía client cho màn hình App HTX (pf/cropcycle, pf/accounts).
// CHỈ để báo lỗi sớm — backend vẫn kiểm lại mọi quy tắc và là nguồn sự thật (R-55).
import type { AdminCropSOP, CropActivity, CropSOPStepInput, CropSOPStage } from "@/api/types";

export const MSG_HARVEST_STEP = "Quy trình phải có bước thu hoạch để kế hoạch sản xuất có điểm kết thúc.";
export const MSG_SOWING_WINDOW = "Ngày gieo sạ phải trong khoảng 15 ngày trước đến 30 ngày sau ngày hiện tại.";

// Nhãn theo BRD FN-01 (AH-8); mã cũ giữ nguyên vì nhật ký và app đang dùng.
export const ACTIVITY_LABEL: Record<CropActivity, string> = {
  lam_dat: "Làm đất",
  gieo_sa: "Gieo trồng",
  tuoi_nuoc: "Quản lý nước",
  bon_phan: "Bón phân",
  phun_thuoc: "Phun thuốc - dưỡng",
  kiem_tra: "Theo dõi",
  thu_hoach: "Thu hoạch",
  cat_tia: "Cắt tỉa",
  khac: "Khác",
};
export const ACTIVITIES = Object.keys(ACTIVITY_LABEL) as CropActivity[];

export function activityLabel(code: string | null | undefined): string {
  return code ? (ACTIVITY_LABEL[code as CropActivity] ?? code) : "—";
}

/** Nhãn vị trí (HTX US-GPS-01/02, US-LOG-05 AC-1). */
export const LOCATION_LABEL: Record<string, { text: string; cls: string }> = {
  match: { text: "Vị trí khớp", cls: "c-ok" },
  mismatch: { text: "Vị trí không khớp", cls: "c-bad" },
  missing: { text: "Thiếu GPS", cls: "c-warn" },
};

export function locationLabel(status: string | null | undefined): { text: string; cls: string } {
  return LOCATION_LABEL[status ?? ""] ?? { text: status ?? "—", cls: "c-mute" };
}

export const SEASON_STATUS: Record<string, { text: string; cls: string }> = {
  open: { text: "Đang canh tác", cls: "c-info" },
  completed: { text: "Đã hoàn thành vụ", cls: "c-ok" },
  cancelled: { text: "Đã hủy", cls: "c-mute" },
  closed: { text: "Đã hoàn thành vụ", cls: "c-ok" },
};

export const LOG_STATUS: Record<string, { text: string; cls: string }> = {
  pending: { text: "Chờ duyệt", cls: "c-warn" },
  approved: { text: "Đã duyệt", cls: "c-ok" },
  needs_supplement: { text: "Cần bổ sung", cls: "c-bad" },
  superseded: { text: "Đã thay thế", cls: "c-mute" },
};

export const TASK_STATUS: Record<string, { text: string; cls: string }> = {
  new: { text: "Mới — chờ phân công", cls: "c-warn" },
  assigned: { text: "Đã giao cán bộ", cls: "c-info" },
  accepted: { text: "Cán bộ đã nhận", cls: "c-info" },
  in_progress: { text: "Đang xử lý", cls: "c-violet" },
  resolved: { text: "Đã xử lý", cls: "c-ok" },
};

export const SUPPORT_CATEGORY: Record<string, string> = {
  dich_hai: "Dịch hại",
  benh_lua: "Bệnh lúa",
  thoi_tiet: "Thời tiết bất thường",
  ky_thuat: "Vướng mắc kỹ thuật",
  may_moc: "Máy móc",
  khac: "Khác",
  sau_benh: "Sâu bệnh",
};

export const GRANT_STATUS: Record<string, { text: string; cls: string }> = {
  active: { text: "Chờ kích hoạt", cls: "c-warn" },
  used: { text: "Đã kích hoạt", cls: "c-ok" },
  revoked: { text: "Đã thu hồi", cls: "c-mute" },
  expired: { text: "Hết hạn", cls: "c-bad" },
};

/** Lý do sửa/hủy/yêu cầu bổ sung: bỏ khoảng trắng hai đầu, tối thiểu `min` ký tự. null = hợp lệ. */
export function reasonError(value: string, min = 10): string | null {
  const n = value.trim().length;
  if (n === 0) return "Bắt buộc nhập lý do.";
  if (n < min) return `Lý do tối thiểu ${min} ký tự (hiện ${n}).`;
  return null;
}

// ---------------------------------------------------------------------------------------------------------------------
// Soạn SOP dạng checklist (AH-8, BRD FN-01 B3–B5): giai đoạn → việc → cửa sổ, vật tư. Bản nháp trên màn hình giữ số
// dạng chuỗi (rỗng = chưa nhập); toSopPayload đổi sang thân gửi máy chủ (danh sách việc phẳng).
// ---------------------------------------------------------------------------------------------------------------------
export type SopWindowKind = "date_range" | "open" | "stage" | "none";
export type SopAnchor = "nxg" | "nth" | "ntt";

export const WINDOW_KIND_LABEL: Record<SopWindowKind, string> = { date_range: "Theo ngày", open: "Mở", stage: "Theo giai đoạn", none: "Không ngày" };
export const WINDOW_KINDS = Object.keys(WINDOW_KIND_LABEL) as SopWindowKind[];
export const ANCHOR_LABEL: Record<SopAnchor, string> = { nxg: "NXG — mốc gốc", nth: "NTH — ngày thu hoạch dự kiến", ntt: "NTT — ngày thu hoạch thực tế" };
export const ANCHORS = Object.keys(ANCHOR_LABEL) as SopAnchor[];

export const SOP_MIN_OFFSET = -365;
export const SOP_MAX_OFFSET = 1825;
export const SOP_MAX_STAGES = 30;
export const SOP_MAX_STEPS = 100;
export const SOP_MAX_MATERIALS = 20;

export interface SopMaterialDraft {
  materialCode: string;
  name: string;
  dose: string;
  unit: string;
}
export interface SopWindowDraft {
  kind: SopWindowKind;
  anchor: SopAnchor;
  from: string;
  to: string;
}
export interface SopTaskDraft {
  name: string;
  activity: CropActivity;
  window: SopWindowDraft;
  materials: SopMaterialDraft[];
  seasonSummary: boolean;
  mandatory: boolean;
  phiDays: string;
  // Chưa sửa được trên màn nhưng phải giữ nguyên qua lần đọc–lưu (bảo toàn dữ liệu; app di động hiện hướng dẫn).
  instruction: string;
  evidenceKinds: NonNullable<CropSOPStepInput["evidence_kinds"]>;
  stageCode: string;
}
export interface SopStageDraft {
  name: string;
  from: string;
  to: string;
  tasks: SopTaskDraft[];
}
export interface SopDraft {
  code: string;
  name: string;
  crop: string;
  varietyCode: string;
  anchorName: string;
  stages: SopStageDraft[];
}
/** Lỗi theo trường; path cùng dạng đường dẫn của máy chủ (stages[0].name, steps[2].window.to, steps[2].materials[0].dose_per_ha). */
export interface SopIssue {
  path: string;
  message: string;
}

export const blankMaterial = (): SopMaterialDraft => ({ materialCode: "", name: "", dose: "", unit: "" });
export const blankTask = (): SopTaskDraft => ({
  name: "",
  activity: "kiem_tra",
  window: { kind: "none", anchor: "nxg", from: "", to: "" },
  materials: [],
  seasonSummary: false,
  mandatory: false,
  phiDays: "",
  instruction: "",
  evidenceKinds: [],
  stageCode: "",
});
export const blankStage = (): SopStageDraft => ({ name: "", from: "", to: "", tasks: [] });
export const blankSop = (): SopDraft => ({ code: "", name: "", crop: "", varietyCode: "", anchorName: "", stages: [blankStage()] });

/** Số nguyên người gõ: "" → null, sai dạng → NaN. */
function intOf(raw: string): number | null {
  const s = raw.trim();
  if (s === "") return null;
  return /^-?\d+$/.test(s) ? Number(s) : Number.NaN;
}
const inOffsetRange = (n: number) => n >= SOP_MIN_OFFSET && n <= SOP_MAX_OFFSET;
const MSG_OFFSET = `Trong khoảng ${SOP_MIN_OFFSET} … ${SOP_MAX_OFFSET} ngày so với mốc.`;
const nameKey = (s: string) => s.trim().replace(/\s+/g, " ").toLowerCase();

/** Chỉ số phẳng (steps[j]) của việc ti trong giai đoạn si. */
export function flatIndex(stages: readonly SopStageDraft[], si: number, ti: number): number {
  let j = 0;
  for (let i = 0; i < si; i++) j += stages[i]?.tasks.length ?? 0;
  return j + ti;
}

/** Giai đoạn đã có khoảng ngày Từ–Đến (điều kiện để việc chọn cửa sổ "Theo giai đoạn"). */
export function stageHasRange(s: Pick<SopStageDraft, "from" | "to">): boolean {
  return s.from.trim() !== "" && s.to.trim() !== "";
}

/**
 * Kiểm bản nháp SOP trước khi gửi (AC2, AC3, AC5): cùng luật và cùng đường dẫn trường với máy chủ — máy chủ vẫn
 * kiểm lại tất cả. forPublish thêm luật chặn phát hành (EX-05, Q-06).
 */
export function validateSop(d: SopDraft, forPublish = false): SopIssue[] {
  const out: SopIssue[] = [];
  const add = (path: string, message: string) => out.push({ path, message });
  if (!d.name.trim()) add("name", "Nhập tên SOP.");
  else if (d.name.trim().length > 200) add("name", "Tên SOP tối đa 200 ký tự.");
  if (!d.crop.trim()) add("crop", "Chọn loại cây.");
  if (!d.anchorName.trim()) add("anchor_name", "Nhập tên mốc gốc, ví dụ Ngày xuống giống.");
  else if (d.anchorName.trim().length > 100) add("anchor_name", "Tên mốc gốc tối đa 100 ký tự.");
  if (d.stages.length > SOP_MAX_STAGES) add("stages", `Tối đa ${SOP_MAX_STAGES} giai đoạn.`);
  const total = d.stages.reduce((n, s) => n + s.tasks.length, 0);
  if (total > SOP_MAX_STEPS) add("steps", `Tối đa ${SOP_MAX_STEPS} việc.`);

  const stageNames = new Set<string>();
  d.stages.forEach((s, i) => {
    const f = `stages[${i}]`;
    const nm = s.name.trim();
    if (!nm || nm.length > 200) add(`${f}.name`, "Nhập tên giai đoạn (tối đa 200 ký tự).");
    else if (stageNames.has(nameKey(nm))) add(`${f}.name`, "Tên giai đoạn trùng.");
    else stageNames.add(nameKey(nm));
    const from = intOf(s.from);
    const to = intOf(s.to);
    if ((from === null) !== (to === null)) add(from === null ? `${f}.from_day` : `${f}.to_day`, "Nhập cả Từ và Đến hoặc để trống cả hai.");
    if (from !== null && Number.isNaN(from)) add(`${f}.from_day`, "Từ phải là số nguyên (cho phép số âm).");
    else if (from !== null && !inOffsetRange(from)) add(`${f}.from_day`, MSG_OFFSET);
    if (to !== null && Number.isNaN(to)) add(`${f}.to_day`, "Đến phải là số nguyên (cho phép số âm).");
    else if (to !== null && !inOffsetRange(to)) add(`${f}.to_day`, MSG_OFFSET);
    if (from !== null && to !== null && !Number.isNaN(from) && !Number.isNaN(to) && from > to) add(`${f}.to_day`, "Đến phải lớn hơn hoặc bằng Từ.");
  });

  let summary = false;
  d.stages.forEach((s, si) => {
    const taskNames = new Set<string>();
    s.tasks.forEach((t, ti) => {
      const f = `steps[${flatIndex(d.stages, si, ti)}]`;
      const nm = t.name.trim();
      if (!nm || nm.length > 200) add(`${f}.name`, "Nhập nội dung việc (tối đa 200 ký tự).");
      else if (taskNames.has(nameKey(nm))) add(`${f}.name`, "Nội dung việc trùng trong cùng giai đoạn.");
      else taskNames.add(nameKey(nm));
      if (t.seasonSummary) {
        if (summary) add(`${f}.season_summary`, "Chỉ một việc được đánh dấu tổng kết vụ.");
        summary = true;
      }
      const phi = intOf(t.phiDays);
      if (phi !== null && (Number.isNaN(phi) || phi < 0 || phi > 120)) add(`${f}.phi_days`, "Thời gian cách ly trong 0 … 120 ngày.");
      validateWindow(t.window, s, f, add);
      validateMaterials(t.materials, f, add);
    });
  });

  if (forPublish) out.push(...publishIssues(d));
  return out;
}

function validateWindow(w: SopWindowDraft, stage: SopStageDraft, f: string, add: (p: string, m: string) => void): void {
  const from = intOf(w.from);
  const to = intOf(w.to);
  const bad = (n: number | null) => n !== null && (Number.isNaN(n) || !inOffsetRange(n));
  if (w.kind === "date_range" || w.kind === "open") {
    if (from === null) add(`${f}.window.from`, "Nhập Từ (số nguyên, cho phép số âm).");
    else if (bad(from)) add(`${f}.window.from`, Number.isNaN(from) ? "Từ phải là số nguyên." : MSG_OFFSET);
  }
  if (w.kind === "date_range") {
    if (to === null) add(`${f}.window.to`, "Nhập Đến (số nguyên, cho phép số âm).");
    else if (bad(to)) add(`${f}.window.to`, Number.isNaN(to) ? "Đến phải là số nguyên." : MSG_OFFSET);
    else if (from !== null && !Number.isNaN(from) && from > to) add(`${f}.window.to`, "Đến phải lớn hơn hoặc bằng Từ.");
  }
  if (w.kind === "stage" && !stageHasRange(stage)) add(`${f}.window.kind`, "Giai đoạn chứa việc chưa có khoảng ngày Từ–Đến.");
}

function validateMaterials(ms: readonly SopMaterialDraft[], f: string, add: (p: string, m: string) => void): void {
  if (ms.length > SOP_MAX_MATERIALS) add(`${f}.materials`, `Tối đa ${SOP_MAX_MATERIALS} dòng vật tư.`);
  const seen = new Set<string>();
  ms.forEach((m, k) => {
    const g = `${f}.materials[${k}]`;
    const key = m.materialCode ? `c:${m.materialCode}` : m.name.trim() ? `n:${nameKey(m.name)}` : "";
    if (!key) add(`${g}.name`, "Chọn vật tư trong danh mục hoặc nhập tên.");
    else if (seen.has(key)) add(`${g}.material_code`, "Vật tư lặp lại trong cùng một việc.");
    else seen.add(key);
    const dose = parseDecimalInput(m.dose, 4);
    if (dose === null) add(`${g}.dose_per_ha`, "Liều theo ha là số, tối đa 4 chữ số lẻ.");
    else if (!/[1-9]/.test(dose)) add(`${g}.dose_per_ha`, "Liều phải lớn hơn 0.");
    else if (Number(dose) > 1_000_000) add(`${g}.dose_per_ha`, "Liều tối đa 1.000.000.");
    if (!m.unit.trim() || m.unit.trim().length > 20) add(`${g}.unit`, "Đơn vị bắt buộc, tối đa 20 ký tự.");
  });
}

/** Luật chặn phát hành (AC5, EX-05, Q-06); đường dẫn trỏ tới việc hoặc giai đoạn cần sửa. */
function publishIssues(d: SopDraft): SopIssue[] {
  const total = d.stages.reduce((n, s) => n + s.tasks.length, 0);
  if (d.stages.length === 0 || total === 0) return [{ path: "stages", message: "Quy trình chưa có giai đoạn hoặc việc — chưa phát hành được." }];
  const empty = d.stages.findIndex((s) => s.tasks.length === 0);
  if (empty >= 0) return [{ path: `stages[${empty}]`, message: `Giai đoạn ${empty + 1} chưa có việc nào — thêm việc hoặc xóa giai đoạn.` }];
  let last: { j: number; t: SopTaskDraft; si: number } | null = null;
  d.stages.forEach((s, si) =>
    s.tasks.forEach((t, ti) => {
      if (t.activity === "thu_hoach") last = { j: flatIndex(d.stages, si, ti), t, si };
    }),
  );
  if (!last) return [{ path: "steps", message: MSG_HARVEST_STEP }];
  const { j, t, si } = last as { j: number; t: SopTaskDraft; si: number };
  const ok = t.window.kind === "stage" ? stageHasRange(d.stages[si]!) : t.window.kind === "date_range" && t.window.anchor === "nxg";
  return ok ? [] : [{ path: `steps[${j}].window`, message: `Việc Thu hoạch cuối (${t.name.trim() || `việc ${j + 1}`}) cần cửa sổ Theo ngày so với mốc gốc hoặc Theo giai đoạn.` }];
}

/** Dựng thân gửi máy chủ: giai đoạn và danh sách việc phẳng, đánh seq, stage_seq, task_seq theo thứ tự trên màn hình. */
export function toSopPayload(d: SopDraft): { stages: CropSOPStage[]; steps: CropSOPStepInput[] } {
  const stages: CropSOPStage[] = d.stages.map((s, i) => ({
    seq: i + 1,
    name: s.name.trim(),
    from_day: intOf(s.from),
    to_day: intOf(s.to),
  }));
  const steps: CropSOPStepInput[] = [];
  d.stages.forEach((s, si) =>
    s.tasks.forEach((t, ti) => {
      const w = t.window;
      const window =
        w.kind === "date_range"
          ? { kind: w.kind, anchor: w.anchor, from: intOf(w.from) ?? 0, to: intOf(w.to) ?? 0 }
          : w.kind === "open"
            ? { kind: w.kind, anchor: w.anchor, from: intOf(w.from) ?? 0 }
            : { kind: w.kind };
      const phi = intOf(t.phiDays);
      steps.push({
        seq: steps.length + 1,
        stage_seq: si + 1,
        task_seq: ti + 1,
        name: t.name.trim(),
        activity_code: t.activity,
        window,
        materials: t.materials.map((m) => ({
          ...(m.materialCode ? { material_code: m.materialCode } : { name: m.name.trim() }),
          dose_per_ha: parseDecimalInput(m.dose, 4) ?? m.dose,
          unit: m.unit.trim(),
        })),
        season_summary: t.seasonSummary,
        mandatory: t.mandatory,
        ...(phi !== null && !Number.isNaN(phi) ? { phi_days: phi } : {}),
        ...(t.instruction !== "" ? { instruction: t.instruction } : {}),
        ...(t.evidenceKinds.length > 0 ? { evidence_kinds: t.evidenceKinds } : {}),
        ...(t.stageCode !== "" ? { stage_code: t.stageCode } : {}),
      });
    }),
  );
  return { stages, steps };
}

/** Đọc một phiên bản đã lưu thành bản nháp trên màn hình (nhóm việc theo giai đoạn). */
export function fromSopVersion(template: Pick<AdminCropSOP, "code" | "name" | "crop" | "variety_code" | "anchor_name">, version: AdminCropSOP["versions"][number]): SopDraft {
  const stages: SopStageDraft[] = (version.stages.length > 0 ? version.stages : [{ seq: 1, name: "Giai đoạn 1", from_day: null, to_day: null }]).map((s) => ({
    name: s.name,
    from: s.from_day == null ? "" : String(s.from_day),
    to: s.to_day == null ? "" : String(s.to_day),
    tasks: [],
  }));
  for (const st of version.steps) {
    const target = stages[Math.min(Math.max((st.stage_seq ?? 1) - 1, 0), stages.length - 1)]!;
    const w = st.window;
    target.tasks.push({
      name: st.name,
      activity: st.activity_code,
      window: { kind: (w?.kind ?? "none") as SopWindowKind, anchor: (w?.anchor ?? "nxg") as SopAnchor, from: w?.from == null ? "" : String(w.from), to: w?.to == null ? "" : String(w.to) },
      materials: (st.materials ?? []).map((m) => ({ materialCode: m.material_code ?? "", name: m.name ?? "", dose: m.dose_per_ha, unit: m.unit })),
      seasonSummary: st.season_summary === true,
      mandatory: st.mandatory,
      phiDays: st.phi_days == null ? "" : String(st.phi_days),
      instruction: st.instruction ?? "",
      evidenceKinds: st.evidence_kinds ?? [],
      stageCode: st.stage_code ?? "",
    });
  }
  return { code: template.code, name: template.name, crop: template.crop, varietyCode: template.variety_code ?? "", anchorName: template.anchor_name, stages };
}

/** Đổi chỗ phần tử i với i+delta. Ngoài biên → bản sao giữ nguyên. */
export function moveItem<T>(items: readonly T[], i: number, delta: -1 | 1): T[] {
  const j = i + delta;
  if (i < 0 || j < 0 || i >= items.length || j >= items.length) return [...items];
  const next = [...items];
  [next[i], next[j]] = [next[j]!, next[i]!];
  return next;
}

/**
 * SĐT chủ hộ → dạng chuẩn 0xxxxxxxxx (tên đăng nhập tài khoản nông dân). Nhận +84…, 84…, có dấu cách/chấm/gạch.
 * Sai định dạng → null.
 */
export function normalizePhone(raw: string): string | null {
  const digits = raw.replace(/[\s.\-()]/g, "");
  let s = digits;
  if (s.startsWith("+84")) s = `0${s.slice(3)}`;
  else if (/^84\d{9}$/.test(s)) s = `0${s.slice(2)}`;
  return /^0\d{9}$/.test(s) ? s : null;
}

/**
 * Chuỗi số nhập tay (chấp nhận dấu phẩy thập phân kiểu Việt) → chuỗi thập phân chuẩn "123.45".
 * Không qua float (R-10). Quá `maxFrac` chữ số lẻ, âm (khi !allowNegative) hoặc sai dạng → null.
 */
export function parseDecimalInput(raw: string, maxFrac = 3, allowNegative = false): string | null {
  const s = raw.trim().replace(/\s/g, "").replace(",", ".");
  const m = /^(-?)(\d+)(?:\.(\d*))?$/.exec(s);
  if (!m) return null;
  if (m[1] === "-" && !allowNegative) return null;
  const frac = (m[3] ?? "").replace(/0+$/, "");
  if (frac.length > maxFrac) return null;
  const int = m[2]!.replace(/^0+(?=\d)/, "");
  return `${m[1]}${int}${frac ? `.${frac}` : ""}`;
}

/** Ngày YYYY-MM-DD → số ngày kể từ 1970-01-01 (không phụ thuộc múi giờ). */
function dayNumber(iso: string): number | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso);
  if (!m) return null;
  const t = Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3]));
  const d = new Date(t);
  if (d.getUTCMonth() !== Number(m[2]) - 1 || d.getUTCDate() !== Number(m[3])) return null;
  return Math.round(t / 86_400_000);
}

/** Ngày nghiệp vụ hôm nay theo giờ Việt Nam (R-12) dạng YYYY-MM-DD. */
export function todayVN(now: Date = new Date()): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Ho_Chi_Minh", year: "numeric", month: "2-digit", day: "2-digit" }).format(now);
}

/** Cộng n ngày vào ngày YYYY-MM-DD. */
export function addDays(iso: string, n: number): string {
  const d = dayNumber(iso);
  if (d === null) return iso;
  return new Date((d + n) * 86_400_000).toISOString().slice(0, 10);
}

/** Ngày sạ trong [hôm nay − 15, hôm nay + 30] (HTX US-SEASON-01 AC-2). null = hợp lệ. */
export function sowingDateError(date: string, today: string = todayVN()): string | null {
  const d = dayNumber(date);
  const t = dayNumber(today);
  if (d === null || t === null) return "Ngày không hợp lệ.";
  return d < t - 15 || d > t + 30 ? MSG_SOWING_WINDOW : null;
}

export interface HarvestDraft {
  mass: string;
  date: string;
}

/** Một dòng bảng nhập nhanh sản lượng (HTX US-YIELD-02). Trống cả hai = bỏ qua dòng (null, null). */
export function harvestRowError(row: HarvestDraft, today: string = todayVN()): string | null {
  if (!row.mass.trim()) return "Nhập sản lượng (kg).";
  const kg = parseDecimalInput(row.mass, 3, true);
  if (kg === null) return "Sản lượng phải là số, tối đa 3 chữ số lẻ.";
  if (kg.startsWith("-")) return "Sản lượng không được âm.";
  if (row.date) {
    const d = dayNumber(row.date);
    const t = dayNumber(today);
    if (d === null) return "Ngày thu hoạch không hợp lệ.";
    if (t !== null && d > t) return "Ngày thu hoạch không được ở tương lai.";
  }
  return null;
}

/** Năng suất t/ha chỉ để HIỂN THỊ trước khi lưu (backend tính số chính thức). */
export function previewYield(massKg: string, areaHa: string): string | null {
  const kg = Number(massKg);
  const ha = Number(areaHa);
  if (!Number.isFinite(kg) || !Number.isFinite(ha) || ha <= 0) return null;
  return (kg / 1000 / ha).toFixed(2);
}

/** Ô bắt đầu bằng = + - @ bị Excel hiểu là công thức (chèn công thức CSV) → thêm dấu nháy. */
function csvCell(v: string | number | null | undefined): string {
  let s = v === null || v === undefined ? "" : String(v);
  if (/^[=+\-@\t\r]/.test(s) && !/^-?\d+([.,]\d+)?$/.test(s)) s = `'${s}`;
  return /[",\r\n;]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
}

/** CSV UTF-8 có BOM (Excel mở đúng tiếng Việt), phân cách dấu phẩy, xuống dòng CRLF. */
export function toCsv(rows: readonly (readonly (string | number | null | undefined)[])[]): string {
  return `﻿${rows.map((r) => r.map(csvCell).join(",")).join("\r\n")}\r\n`;
}
