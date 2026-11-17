// BACKEND GIẢ LẬP — đồng ý thu thập ảnh GPS và dữ liệu sản xuất (AH-23, REQ-011, KH-24): GET/POST /v1/me/data-consent.
// Tài khoản HTX (coop_manager, htx_member — AH-39) phải đồng ý phiên bản nội dung hiện hành; vai khác (Admin…) luôn
// required=false. Persona `htx` và `htx-member` mặc định ĐÃ đồng ý "tam-1" để các luồng thử hiện có không đổi;
// resetMockDataConsent() xóa để thử lần đầu.
import { hasRole, json, problem, validation, type Route } from "./http";

/** Phiên bản và nội dung tạm — khớp tham số seed accounts.data_consent.* của máy chủ. */
export const MOCK_DATA_CONSENT = {
  version: "tam-1",
  text: "Ứng dụng HTX thu thập ảnh hiện trường có gắn tọa độ GPS và dữ liệu sản xuất (vụ, nhật ký canh tác, sản lượng) của các nông hộ do HTX của bạn quản lý, để lập hồ sơ sản xuất và phục vụ báo cáo Đề án 1 triệu ha lúa. Dữ liệu được lưu và xử lý theo Nghị định 13/2023/NĐ-CP. Bấm Đồng ý để tiếp tục sử dụng ứng dụng.",
} as const;

const SEED_ACTORS: readonly string[] = ["mock-htx-0001", "mock-htx-member-0001"];
const SEED_CONSENTED_AT = "2026-11-15T00:00:00Z";

interface ConsentRecord {
  id: string;
  version: string;
  consented_at: string;
}

/** Bằng chứng theo tài khoản (actor_ref) → phiên bản → bản ghi; chỉ thêm. */
const store = new Map<string, Map<string, ConsentRecord>>();
let seeded = true;
let nextId = 1;

/** Xóa mọi bằng chứng (kể cả bản gieo sẵn của persona `htx`, `htx-member`): tài khoản HTX mới, chưa đồng ý. */
export function resetMockDataConsent(): void {
  store.clear();
  seeded = false;
  nextId = 1;
}

function recordFor(actor: string, version: string): ConsentRecord | undefined {
  const own = store.get(actor)?.get(version);
  if (own) return own;
  if (seeded && SEED_ACTORS.includes(actor) && version === MOCK_DATA_CONSENT.version) {
    return { id: "0192f0e0-0000-7000-8000-00000000c001", version, consented_at: SEED_CONSENTED_AT };
  }
  return undefined;
}

export const dataConsentAppRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/me\/data-consent$/,
    handler: ({ persona }) => {
      if (!hasRole(persona, "coop_manager", "htx_member")) return json({ required: false, version: null, text: null, consented_at: null });
      const prev = recordFor(persona.me.actor_ref, MOCK_DATA_CONSENT.version);
      if (prev) return json({ required: false, version: prev.version, text: null, consented_at: prev.consented_at });
      return json({ required: true, version: MOCK_DATA_CONSENT.version, text: MOCK_DATA_CONSENT.text, consented_at: null });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/me\/data-consent$/,
    handler: ({ persona, body }) => {
      if (!hasRole(persona, "coop_manager", "htx_member")) return problem(403, "forbidden", "Forbidden");
      const version = body && typeof body === "object" ? (body as { version?: unknown }).version : undefined;
      if (typeof version !== "string" || !/^[A-Za-z0-9_.-]{1,40}$/u.test(version)) {
        return validation([{ field: "version", reason: "Mã phiên bản nội dung không hợp lệ" }]);
      }
      if (version !== MOCK_DATA_CONSENT.version) {
        return problem(409, "accounts.data_consent_version_stale", "Nội dung đã được cập nhật. Vui lòng đọc lại và xác nhận.");
      }
      const prev = recordFor(persona.me.actor_ref, version);
      if (prev) return json(prev, 200);
      const rec: ConsentRecord = {
        id: `0192f0e0-0000-7000-8000-${String(nextId++).padStart(12, "0")}`,
        version,
        consented_at: new Date().toISOString(),
      };
      const own = store.get(persona.me.actor_ref) ?? new Map<string, ConsentRecord>();
      own.set(version, rec);
      store.set(persona.me.actor_ref, own);
      return json(rec, 201);
    },
  },
];
