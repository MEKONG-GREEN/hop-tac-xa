import { describe, expect, it } from "vitest";

import type { Notification } from "@/api/types";
import { dayLabel, groupByDay, KIND_LABEL, timeOfDay, vnDayKey } from "./notify";

// Gói NOTIF: chuông thông báo hiện nhãn tiếng Việt cho các loại thông báo Báo giá nông sản (pf 20261013070025).
describe("KIND_LABEL — Báo giá nông sản", () => {
  it("có nhãn tiếng Việt cho mọi loại price_*", () => {
    const kinds = [
      "price_reminder",
      "price_alert",
      "price_verify_request",
      "price_verify_reply",
      "price_verify_decision",
      "price_lock_summary",
      "price_board_approved",
      "price_board_reopened",
      "price_proxy_entry",
    ] as const;
    for (const k of kinds) {
      expect(KIND_LABEL[k]).toBeTruthy();
      expect(KIND_LABEL[k]).not.toBe(k);
    }
    expect(KIND_LABEL.price_verify_request).toBe("Yêu cầu xác minh giá");
    expect(KIND_LABEL.price_lock_summary).toBe("Tổng hợp khóa nhập giá");
  });

  it("W11.53b: có nhãn tiếng Việt cho ba loại thông báo biểu dự án KNQG (enum hợp đồng)", () => {
    for (const k of ["cnts_knqg_sent", "cnts_knqg_reviewed", "cnts_knqg_submitted"] as const) {
      expect(KIND_LABEL[k]).toBeTruthy();
      expect(KIND_LABEL[k]).not.toBe(k);
    }
  });
});

// AH-22 AC1: trung tâm thông báo nhóm theo ngày giờ Việt Nam, có giờ tạo và nhãn bốn loại mới.
describe("AH-22 AC1 — nhóm theo ngày (giờ Việt Nam)", () => {
  const NOW = Date.parse("2026-10-01T03:00:00Z"); // 10:00 ngày 01/10 giờ Việt Nam
  const mk = (id: string, created_at: string) => ({ id, created_at }) as unknown as Notification;

  it("AH22_AC1: mốc 23:30Z thuộc ngày hôm sau theo giờ Việt Nam; nhãn Hôm nay/Hôm qua/dd/mm/yyyy; giữ thứ tự", () => {
    const items = [mk("a", "2026-09-30T20:00:00Z"), mk("b", "2026-09-30T17:30:00Z"), mk("c", "2026-09-30T05:00:00Z"), mk("d", "2026-09-27T01:00:00Z")];
    const groups = groupByDay(items, NOW);
    expect(groups.map((g) => g.key)).toEqual(["2026-10-01", "2026-09-30", "2026-09-27"]);
    expect(groups.map((g) => g.label)).toEqual(["Hôm nay", "Hôm qua", "27/09/2026"]);
    expect(groups[0]!.items.map((n) => n.id)).toEqual(["a", "b"]);
    expect(groups[1]!.items.map((n) => n.id)).toEqual(["c"]);
    expect(groupByDay([], NOW)).toEqual([]);
  });

  it("AH22_AC1: vnDayKey và timeOfDay theo giờ Việt Nam", () => {
    expect(vnDayKey("2026-09-30T23:30:00Z")).toBe("2026-10-01");
    expect(timeOfDay("2026-09-30T23:30:00Z")).toBe("06:30");
    expect(timeOfDay("2026-10-01T03:05:00Z")).toBe("10:05");
    expect(vnDayKey("không phải ngày")).toBe("");
    expect(dayLabel("", NOW)).toBe("Không rõ ngày");
  });

  it("AH22_AC1: có nhãn cho bốn loại thông báo Thành viên HTX", () => {
    expect(KIND_LABEL.season_calendar_published).toBe("Lịch thời vụ");
    expect(KIND_LABEL.season_task_due).toBe("Nhắc việc");
    expect(KIND_LABEL.season_log_missing).toBe("Nhắc ghi nhật ký");
    expect(KIND_LABEL.season_process_changed).toBe("Quy trình thay đổi");
  });
});
