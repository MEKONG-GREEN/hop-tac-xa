import type { ApiPaths } from "../client";
import { uuidv7 } from "../idempotency";
import { COOPERATIVES, htxCoop } from "./data";
import { hasRole, json, problem, validation, type Ctx, type Route } from "./http";
import { TENANT, type MockPersona } from "./personas";

// FN-09 (AH-278) và FN-06 (AH-274): yêu cầu chia sẻ có hướng, chỉ Đại diện HTX quyết định; "approved" cấp quyền
// đọc chỉ xem, thu hồi mất quyền ở yêu cầu kế tiếp. Khớp backend pf/cooperative (native_sharing).
type Sharing = ApiPaths["/v1/htx-sharing"]["get"]["responses"][200]["content"]["application/json"]["items"][number];
type State = Sharing["state"];
const requests = new Map<string, { tenant: string; row: Sharing }>();
const receipts = new Map<string, { input: string; row: Sharing }>();
const headers = { "Cache-Control": "no-store" };
const nameOf = (ref: string) => COOPERATIVES.find((r) => r.api.ref === ref)?.api.name;

/** Thêm một yêu cầu vào trạng thái mock (kiểm thử, dữ liệu demo). */
export function seedNativeSharing(requester: string, owner: string, state: State, tenant = TENANT): Sharing {
  const at = new Date(Date.now() - 86_400_000).toISOString();
  const row: Sharing = {
    ref: uuidv7(), requester_cooperative_ref: requester, owner_cooperative_ref: owner, requester_name: nameOf(requester), owner_name: nameOf(owner),
    state, revision: state === "requested" ? 1 : 2, requested_at: at, processed_at: state === "requested" ? null : at, actor_ref: uuidv7(),
  };
  requests.set(row.ref, { tenant, row });
  return row;
}

/** Xóa trạng thái (kiểm thử). */
export function resetNativeSharingMock() { requests.clear(); receipts.clear(); }

/** Demo: một yêu cầu gửi đến chờ duyệt và một HTX đã duyệt chia sẻ cho HTX của persona Đại diện HTX. */
export function seedNativeSharingDemo() {
  const own = htxCoop().api.ref;
  const others = COOPERATIVES.filter((c) => c.api.active && c.api.ref !== own);
  if (others[0]) seedNativeSharing(others[0].api.ref, own, "requested");
  if (others[1]) seedNativeSharing(own, others[1].api.ref, "approved");
}
// Kiểm thử bắt đầu từ trạng thái rỗng; bản demo có sẵn dữ liệu để xem luồng.
if (process.env.NODE_ENV !== "test") seedNativeSharingDemo();

function denied(p: MockPersona) {
  if (!hasRole(p, "coop_manager") || hasRole(p, "platform_admin")) return problem(403, "forbidden", "Chỉ Đại diện HTX được quyết định chia sẻ");
  if (!p.me.org?.ref) return problem(403, "node_scope_required", "Tài khoản chưa gắn HTX");
  return null;
}
const visible = (p: MockPersona) => [...requests.values()].filter((g) => g.tenant === p.me.tenant_id && [g.row.requester_cooperative_ref, g.row.owner_cooperative_ref].includes(p.me.org?.ref ?? "")).map((g) => g.row);

/** HTX đã duyệt chia sẻ cho HTX của persona (chỉ Đại diện HTX; Thành viên HTX, Admin: rỗng). */
export function nativeSharedRefs(p: MockPersona): string[] {
  if (denied(p)) return [];
  return visible(p).filter((r) => r.requester_cooperative_ref === p.me.org?.ref && r.state === "approved").map((r) => r.owner_cooperative_ref);
}

/** Persona được đọc (chỉ xem) dữ liệu của HTX coopRef nhờ chia sẻ đang hiệu lực. */
export function nativeSharedRead(p: MockPersona, coopRef: string): boolean {
  return coopRef !== p.me.org?.ref && nativeSharedRefs(p).includes(coopRef);
}

function change(c: Ctx): Response {
  const blocked = denied(c.persona); if (blocked) return blocked;
  const input = (c.body ?? {}) as Record<string, unknown>;
  const create = c.params.length === 0;
  const action = create ? "request" : input.action;
  const ref = c.params[0];
  const key = c.request.headers.get("Idempotency-Key");
  if (!key) return problem(400, "idempotency_key_required", "Thiếu khóa thao tác");
  const op = `${c.persona.me.tenant_id}:${c.persona.me.actor_ref}:${key}`, digest = JSON.stringify({ ref, input });
  const previous = receipts.get(op);
  if (previous) return previous.input === digest ? json(previous.row, create ? 201 : 200, headers) : problem(422, "idempotency_key_reused", "Khóa đã được sử dụng");
  const own = c.persona.me.org!.ref;
  let row: Sharing;
  if (create) {
    if (!input.owner_cooperative_ref) return validation([{ field: "owner_cooperative_ref", reason: "Bắt buộc nhập" }]);
    const target = COOPERATIVES.find((r) => r.api.ref === input.owner_cooperative_ref && r.api.active)?.api;
    if (!target || target.ref === own) return validation([{ field: "owner_cooperative_ref", reason: "Chọn HTX khác đang hoạt động" }]);
    if (visible(c.persona).some((r) => r.requester_cooperative_ref === own && r.owner_cooperative_ref === target.ref && ["requested", "approved"].includes(r.state))) return problem(409, "cooperative.sharing_conflict", "Đã có yêu cầu đang hiệu lực");
    row = { ref: uuidv7(), requester_cooperative_ref: own, owner_cooperative_ref: target.ref, requester_name: nameOf(own), owner_name: target.name, state: "requested", revision: 1, requested_at: new Date().toISOString(), processed_at: null, actor_ref: c.persona.me.actor_ref };
  } else {
    const current = visible(c.persona).find((r) => r.ref === ref);
    if (!current) return problem(404, "cooperative.sharing_not_found", "Không tìm thấy yêu cầu");
    if (current.revision !== input.expected_revision) return problem(409, "cooperative.sharing_conflict", "Yêu cầu vừa thay đổi");
    const recipient = current.owner_cooperative_ref === own;
    const next: State | null = current.state === "requested" && recipient && action === "approve" ? "approved"
      : current.state === "requested" && recipient && action === "reject" ? "rejected"
        : action === "revoke" && (current.state === "approved" || (current.state === "requested" && !recipient)) ? "revoked" : null;
    if (!next) return problem(409, "cooperative.sharing_conflict", "Không được chuyển trạng thái này");
    row = { ...current, state: next, revision: current.revision + 1, processed_at: new Date().toISOString(), actor_ref: c.persona.me.actor_ref };
  }
  requests.set(row.ref, { tenant: c.persona.me.tenant_id, row }); receipts.set(op, { input: digest, row });
  return json(row, create ? 201 : 200, headers);
}

export const nativeSharingRoutes: Route[] = [
  { method: "GET", pattern: /^\/v1\/htx-sharing$/, handler: (c) => denied(c.persona) ?? json({ items: visible(c.persona).sort((a, b) => b.requested_at.localeCompare(a.requested_at)) }, 200, headers) },
  { method: "GET", pattern: /^\/v1\/htx-sharing\/candidates$/, handler: (c) => {
    const blocked = denied(c.persona); if (blocked) return blocked;
    const own = c.persona.me.org?.ref;
    const active = new Set([own, ...visible(c.persona).filter((r) => r.requester_cooperative_ref === own && ["requested", "approved"].includes(r.state)).map((r) => r.owner_cooperative_ref)]);
    return json({ items: COOPERATIVES.filter((r) => r.api.active && !active.has(r.api.ref)).map(({ api: r }) => ({ ref: r.ref, code: r.code, name: r.name })) }, 200, headers);
  } },
  { method: "POST", pattern: /^\/v1\/htx-sharing$/, handler: change },
  { method: "POST", pattern: /^\/v1\/htx-sharing\/([^/]+)\/transitions$/, handler: change },
];
