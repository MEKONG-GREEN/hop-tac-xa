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
// FN-TK-02/EX-04 (AH-223): khớp accounts/app.guardLastRepresentative — thao tác làm HTX không còn Đại diện HTX đang
// hoạt động cần confirm_no_representative=true.
function lastRepresentative(ctx: Ctx, tenant: string, u: AccountsUser) {
  if (ctx.url.searchParams.get("confirm_no_representative") === "true") return null;
  const active = (x: AccountsUser) => x.enabled && !!x.org_ref && (x.roles ?? []).includes("coop_manager");
  if (!active(u) || [...users.values()].some((v) => v.tenant === tenant && v.user.id !== u.id && v.user.org_ref === u.org_ref && active(v.user))) return null;
  return problem(409, "accounts.last_representative", "HTX sẽ không còn người duyệt kết quả lô");
}
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
  // AH-276: sửa (không đổi tên đăng nhập, vai chính), khóa, mở khóa — khớp accounts/app.NativeUpdate, NativeSetLocked.
  { method: "POST", pattern: /^\/v1\/accounts\/htx-users\/([\w-]+)\/revisions$/, handler: (ctx) => {
    const denied = guard(ctx); if (denied) return denied;
    const found = users.get(ctx.params[0] ?? "");
    if (found?.tenant !== ctx.persona.me.tenant_id) return problem(404, "not_found", "Không tìm thấy tài khoản");
    const input = (ctx.body ?? {}) as Record<string, unknown>;
    if (Object.keys(input).some((k) => !["full_name", "phone", "email", "cooperative_ref", "also_member"].includes(k))) return problem(400, "bad_request", "Không sửa được tên đăng nhập hoặc vai trò");
    const u = found.user, main = (u.roles ?? [])[0] ?? "";
    if (typeof input.full_name !== "string" || !input.full_name.trim()) return validation([{ field: "full_name", reason: "Bắt buộc nhập" }]);
    if (typeof input.phone !== "string" || !/^0[0-9]{9}$/.test(input.phone.trim())) return validation([{ field: "phone", reason: "SĐT gồm 10 chữ số, bắt đầu bằng 0" }]);
    if (main !== "platform_admin" && !COOPERATIVES.some((c) => c.api.ref === input.cooperative_ref && c.api.active)) return validation([{ field: "cooperative_ref", reason: "Chọn HTX" }]);
    if (input.also_member === true && main !== "coop_manager") return validation([{ field: "also_member", reason: "Chỉ tài khoản Đại diện HTX được kiêm Thành viên HTX" }]);
    if ([...users.values()].some((v) => v.user.id !== u.id && v.user.phone === input.phone)) return problem(409, "accounts.phone_taken", "SĐT đã được dùng cho tài khoản khác");
    const orgChanged = main !== "platform_admin" && input.cooperative_ref !== u.org_ref;
    if (u.id === ctx.persona.me.actor_ref && orgChanged) return problem(403, "accounts.self_action", "Không tự đổi HTX của chính mình");
    if (orgChanged) { const last = lastRepresentative(ctx, found.tenant, u); if (last) return last; }
    const member = main === "coop_manager" ? (typeof input.also_member === "boolean" ? input.also_member : (u.roles ?? []).includes("htx_member")) : false;
    const next: AccountsUser = { ...u, first_name: input.full_name.trim(), last_name: "", phone: input.phone.trim(), email: typeof input.email === "string" ? input.email : u.email,
      org_ref: main === "platform_admin" ? null : String(input.cooperative_ref), roles: main === "coop_manager" && member ? ["coop_manager", "htx_member"] : [main] };
    users.set(u.id, { tenant: found.tenant, user: next });
    return json({ user: next, replayed: false }, 200, headers);
  } },
  { method: "POST", pattern: /^\/v1\/accounts\/htx-users\/([\w-]+)\/(lock|unlock)$/, handler: (ctx) => {
    const denied = guard(ctx); if (denied) return denied;
    const found = users.get(ctx.params[0] ?? "");
    if (found?.tenant !== ctx.persona.me.tenant_id) return problem(404, "not_found", "Không tìm thấy tài khoản");
    if (found.user.id === ctx.persona.me.actor_ref) return problem(403, "accounts.self_action", "Không được tự khóa, mở khóa tài khoản của chính mình");
    const lock = ctx.params[1] === "lock";
    if (found.user.enabled !== lock) return problem(409, "accounts.state_conflict", lock ? "Tài khoản đã bị khóa." : "Tài khoản đang hoạt động.");
    if (lock) { const last = lastRepresentative(ctx, found.tenant, found.user); if (last) return last; }
    const next: AccountsUser = { ...found.user, enabled: !lock, lock_cause: lock ? "manual" : null };
    users.set(next.id, { tenant: found.tenant, user: next });
    return json({ user: next, replayed: false }, 200, headers);
  } },
  { method: "POST", pattern: /^\/v1\/accounts\/htx-users\/([\w-]+)\/password-reset$/, handler: (ctx) => {
    const denied = guard(ctx); if (denied) return denied;
    const found = (() => { const f = users.get(ctx.params[0] ?? ""); return f?.tenant === ctx.persona.me.tenant_id ? f : null; })();
    if (!found) return problem(404, "not_found", "Không tìm thấy tài khoản");
    if (found.user.id === ctx.persona.me.actor_ref) return problem(403, "accounts.self_action", "Không được tự đặt lại mật khẩu của chính mình");
    if (!found.user.enabled) return problem(409, "accounts.state_conflict", "Tài khoản đã khóa, không đặt lại mật khẩu.");
    users.set(found.user.id, { tenant: found.tenant, user: { ...found.user, required_actions: ["UPDATE_PASSWORD"] } });
    return json({ mode: "temporary_password", temporary_password: uuidv7().replaceAll("-", "").slice(-8), phone_hint: "", replayed: false }, 200, headers);
  } },
];

/** Thêm tài khoản vào mock (kiểm thử). */
export function seedNativeAccount(tenant: string, user: AccountsUser) { users.set(user.id, { tenant, user }); }

// FN-08 (AH-277): Đại diện HTX quản lý Thành viên HTX của HTX mình trên Application API — khớp accounts/app.Coop*.
function coopGuard({ persona }: Ctx) {
  if (!hasRole(persona, "coop_manager") || hasRole(persona, "platform_admin")) return problem(403, "forbidden", "Chỉ Đại diện HTX");
  if (!persona.me.org?.ref) return problem(403, "node_scope_required", "Tài khoản chưa gắn HTX");
  return null;
}
const coopVisible = (ctx: Ctx, u: AccountsUser) =>
  u.id === ctx.persona.me.actor_ref || (u.org_ref === ctx.persona.me.org?.ref && (u.roles ?? []).length === 1 && (u.roles ?? [])[0] === "htx_member");
function coopFind(ctx: Ctx) {
  const found = users.get(ctx.params[0] ?? "");
  return found && found.tenant === ctx.persona.me.tenant_id && coopVisible(ctx, found.user) ? found : null;
}
export const coopAccountRoutes: Route[] = [
  { method: "GET", pattern: /^\/v1\/htx-accounts$/, handler: (ctx) => coopGuard(ctx) ?? json({ items: [...users.values()].filter((v) => v.tenant === ctx.persona.me.tenant_id && coopVisible(ctx, v.user)).map((v) => v.user) }, 200, headers) },
  { method: "POST", pattern: /^\/v1\/htx-accounts$/, handler: (ctx) => {
    const denied = coopGuard(ctx); if (denied) return denied;
    const input = (ctx.body ?? {}) as Record<string, unknown>;
    const roles = Array.isArray(input.roles) ? input.roles : [];
    if (roles.length !== 1 || roles[0] !== "htx_member") return problem(403, "accounts.role_not_grantable", "Đại diện HTX chỉ tạo tài khoản Thành viên HTX");
    if (input.cooperative_ref !== ctx.persona.me.org?.ref) return problem(403, "party.out_of_scope", "Chỉ tạo tài khoản cho HTX của bạn");
    if (typeof input.full_name !== "string" || !input.full_name.trim()) return validation([{ field: "full_name", reason: "Bắt buộc nhập" }]);
    if (typeof input.phone !== "string" || !/^0[0-9]{9}$/.test(input.phone.trim())) return validation([{ field: "phone", reason: "SĐT gồm 10 chữ số, bắt đầu bằng 0" }]);
    if ([...users.values()].some((v) => v.user.phone === input.phone)) return problem(409, "accounts.phone_taken", "SĐT đã được dùng cho tài khoản khác");
    const user: AccountsUser = { id: uuidv7(), username: String(input.username ?? "").trim().toLowerCase(), first_name: input.full_name.trim(), last_name: "", phone: input.phone.trim(), email: typeof input.email === "string" ? input.email : "", email_verified: false, org_ref: String(input.cooperative_ref), roles: ["htx_member"], enabled: true, lock_cause: null, required_actions: ["UPDATE_PASSWORD"], created_at: new Date().toISOString(), tracked: true };
    users.set(user.id, { tenant: ctx.persona.me.tenant_id, user });
    return json({ user, temporary_password: uuidv7().replaceAll("-", "").slice(-8) }, 201, headers);
  } },
  { method: "POST", pattern: /^\/v1\/htx-accounts\/([\w-]+)\/revisions$/, handler: (ctx) => {
    const denied = coopGuard(ctx); if (denied) return denied;
    const found = coopFind(ctx); if (!found) return problem(404, "not_found", "Không tìm thấy tài khoản");
    const input = (ctx.body ?? {}) as Record<string, unknown>;
    const u = found.user;
    if (u.id === ctx.persona.me.actor_ref) {
      const member = typeof input.also_member === "boolean" ? input.also_member : (u.roles ?? []).includes("htx_member");
      const next = { ...u, roles: member ? ["coop_manager", "htx_member"] : ["coop_manager"] };
      users.set(u.id, { tenant: found.tenant, user: next });
      return json({ user: next, replayed: false }, 200, headers);
    }
    if (input.cooperative_ref && input.cooperative_ref !== ctx.persona.me.org?.ref) return problem(403, "party.out_of_scope", "Không đổi HTX của Thành viên HTX");
    if (typeof input.full_name !== "string" || !input.full_name.trim()) return validation([{ field: "full_name", reason: "Bắt buộc nhập" }]);
    if (typeof input.phone !== "string" || !/^0[0-9]{9}$/.test(input.phone.trim())) return validation([{ field: "phone", reason: "SĐT gồm 10 chữ số, bắt đầu bằng 0" }]);
    const next = { ...u, first_name: input.full_name.trim(), last_name: "", phone: input.phone.trim(), email: typeof input.email === "string" ? input.email : u.email };
    users.set(u.id, { tenant: found.tenant, user: next });
    return json({ user: next, replayed: false }, 200, headers);
  } },
  { method: "POST", pattern: /^\/v1\/htx-accounts\/([\w-]+)\/(lock|unlock)$/, handler: (ctx) => {
    const denied = coopGuard(ctx); if (denied) return denied;
    const found = coopFind(ctx); if (!found) return problem(404, "not_found", "Không tìm thấy tài khoản");
    if (found.user.id === ctx.persona.me.actor_ref) return problem(403, "accounts.self_action", "Không được tự khóa, mở khóa tài khoản của chính mình");
    const lock = ctx.params[1] === "lock";
    if (found.user.enabled !== lock) return problem(409, "accounts.state_conflict", lock ? "Tài khoản đã bị khóa." : "Tài khoản đang hoạt động.");
    const next: AccountsUser = { ...found.user, enabled: !lock, lock_cause: lock ? "manual" : null };
    users.set(next.id, { tenant: found.tenant, user: next });
    return json({ user: next, replayed: false }, 200, headers);
  } },
  { method: "POST", pattern: /^\/v1\/htx-accounts\/([\w-]+)\/password-reset$/, handler: (ctx) => {
    const denied = coopGuard(ctx); if (denied) return denied;
    const found = coopFind(ctx);
    if (!found) return problem(404, "not_found", "Không tìm thấy tài khoản");
    if (found.user.id === ctx.persona.me.actor_ref) return problem(403, "accounts.self_action", "Không được tự đặt lại mật khẩu của chính mình");
    if (!found.user.enabled) return problem(409, "accounts.state_conflict", "Tài khoản đã khóa, không đặt lại mật khẩu.");
    users.set(found.user.id, { tenant: found.tenant, user: { ...found.user, required_actions: ["UPDATE_PASSWORD"] } });
    return json({ mode: "temporary_password", temporary_password: uuidv7().replaceAll("-", "").slice(-8), phone_hint: "", replayed: false }, 200, headers);
  } },
];

// AH-281 (FN-10/AC-N4, AC-N5): tự đổi mật khẩu. Giả lập không giữ mật khẩu: "sai-mat-khau" là mật khẩu hiện tại sai.
export const MOCK_WRONG_CURRENT_PASSWORD = "sai-mat-khau";
export const mePasswordRoutes: Route[] = [
  { method: "PUT", pattern: /^\/v1\/me\/password$/, handler: (ctx) => {
    const input = (ctx.body ?? {}) as Record<string, unknown>;
    const cur = typeof input.current_password === "string" ? input.current_password : "";
    const next = typeof input.new_password === "string" ? input.new_password : "";
    const confirm = typeof input.confirm_password === "string" ? input.confirm_password : "";
    const errors: { field: string; reason: string }[] = [];
    if (!cur) errors.push({ field: "current_password", reason: "Nhập mật khẩu hiện tại" });
    if ([...next].length < 6) errors.push({ field: "new_password", reason: "Mật khẩu tối thiểu 06 ký tự" });
    else if (cur && next === cur) errors.push({ field: "new_password", reason: "Mật khẩu mới phải khác mật khẩu hiện tại" });
    if (confirm !== next) errors.push({ field: "confirm_password", reason: "Mật khẩu nhập lại không khớp" });
    if (errors.length) return validation(errors);
    if (cur === MOCK_WRONG_CURRENT_PASSWORD) return validation([{ field: "current_password", reason: "Mật khẩu hiện tại không đúng" }]);
    return new Response(null, { status: 204, headers });
  } },
];
