import { uuidv7 } from "../idempotency";
import type { AccountsUser } from "../types";
import { COOPERATIVES } from "./data";
import { hasRole, json, problem, validation, type Route, type Ctx } from "./http";

const users = new Map<string, { tenant: string; user: AccountsUser }>();
const operations = new Map<string, { input: string; user: AccountsUser }>();
export function resetNativeAccountMock() { users.clear(); operations.clear(); }
function guard({ persona }: Ctx) {
  if (!hasRole(persona, "platform_admin")) return problem(403, "forbidden", "Chỉ Admin được quản lý tài khoản HTX");
  if (!persona.amr.includes("otp")) return problem(403, "mfa_required", "Cần xác thực hai yếu tố");
  return null;
}
const headers = { "Cache-Control": "no-store" };
export const nativeAccountRoutes: Route[] = [
  { method: "GET", pattern: /^\/v1\/accounts\/htx-users$/, handler: (ctx) => guard(ctx) ?? json({ items: [...users.values()].filter((v) => v.tenant === ctx.persona.me.tenant_id).map((v) => v.user) }, 200, headers) },
  { method: "GET", pattern: /^\/v1\/accounts\/htx-users\/([\w-]+)$/, handler: (ctx) => {
    const denied = guard(ctx); if (denied) return denied;
    const found = users.get(ctx.params[0] ?? "");
    return found?.tenant === ctx.persona.me.tenant_id ? json(found.user, 200, headers) : problem(404, "not_found", "Không tìm thấy tài khoản");
  } },
  { method: "POST", pattern: /^\/v1\/accounts\/htx-users$/, handler: (ctx) => {
    const denied = guard(ctx); if (denied) return denied;
    const key = ctx.request.headers.get("Idempotency-Key"); if (!key) return problem(400, "idempotency_key_required", "Thiếu khóa thao tác");
    const op = `${ctx.persona.me.tenant_id}:${ctx.persona.me.actor_ref}:${key}`;
    const digest = JSON.stringify(ctx.body), previous = operations.get(op);
    if (previous) return previous.input !== digest ? problem(422, "idempotency_key_reused", "Khóa đã được sử dụng") : json({ user: previous.user, temporary_password: null }, 200, headers);
    const input = (ctx.body ?? {}) as Record<string, unknown>;
    const roles = Array.isArray(input.roles) ? input.roles.filter((r): r is string => typeof r === "string") : [];
    const admin = roles.length === 1 && roles[0] === "platform_admin";
    if (!(roles.length === 1 && ["platform_admin", "coop_manager", "htx_member"].includes(roles[0] ?? "")) && !(roles.length === 2 && roles.includes("coop_manager") && roles.includes("htx_member"))) return validation([{ field: "roles", reason: "Chọn vai hợp lệ" }]);
    if (typeof input.username !== "string" || !/^[a-z0-9][a-z0-9._@-]{2,63}$/.test(input.username.trim().toLowerCase())) return validation([{ field: "username", reason: "Tên đăng nhập không hợp lệ" }]);
    if (typeof input.full_name !== "string" || !input.full_name.trim()) return validation([{ field: "full_name", reason: "Bắt buộc nhập" }]);
    if (typeof input.phone !== "string" || !/^0[0-9]{9}$/.test(input.phone.trim())) return validation([{ field: "phone", reason: "SĐT gồm 10 chữ số, bắt đầu bằng 0" }]);
    if (!admin && !COOPERATIVES.some((c) => c.api.ref === input.cooperative_ref && c.api.active)) return validation([{ field: "cooperative_ref", reason: "Chọn HTX" }]);
    if (admin && input.cooperative_ref) return validation([{ field: "cooperative_ref", reason: "Admin không gắn HTX" }]);
    if (admin && (typeof input.email !== "string" || !input.email.includes("@"))) return validation([{ field: "email", reason: "Thiếu Email" }]);
    if ([...users.values()].some((v) => v.user.phone === input.phone)) return problem(409, "accounts.phone_taken", "SĐT đã được dùng cho tài khoản khác");
    if ([...users.values()].some((v) => v.user.username === String(input.username).trim().toLowerCase())) return problem(409, "accounts.user_exists", "Tên đăng nhập đã được dùng");
    const user: AccountsUser = { id: uuidv7(), username: input.username.trim().toLowerCase(), first_name: input.full_name.trim(), last_name: "", phone: input.phone.trim(), email: typeof input.email === "string" ? input.email : "", email_verified: !!input.email, org_ref: typeof input.cooperative_ref === "string" ? input.cooperative_ref : null, roles, enabled: true, lock_cause: null, required_actions: ["UPDATE_PASSWORD"], created_at: new Date().toISOString(), tracked: true };
    users.set(user.id, { tenant: ctx.persona.me.tenant_id, user }); operations.set(op, { input: digest, user });
    // Synthetic mock credential only; never stored in users or replay responses.
    return json({ user, temporary_password: uuidv7().replaceAll("-", "").slice(-8) }, 201, headers);
  } },
];
