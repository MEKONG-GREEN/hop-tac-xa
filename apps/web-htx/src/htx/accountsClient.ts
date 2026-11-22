import { unwrap, type AdminPaths, type ApiClient, type AdminApiClient } from "@/api/client";
import type { AccountsUser } from "@/api/types";

/**
 * Tài khoản realm HTX theo người dùng màn: Admin (FN-07, Admin API `/v1/accounts/htx-users`) hoặc Đại diện HTX (FN-08,
 * AH-277, Application API `/v1/htx-accounts` — chỉ Thành viên HTX của HTX mình và dòng của chính mình).
 */
export type AccountsScope = "admin" | "coop";
export type AccountInput = AdminPaths["/v1/accounts/htx-users"]["post"]["requestBody"]["content"]["application/json"];
export type AccountRevision = AdminPaths["/v1/accounts/htx-users/{id}/revisions"]["post"]["requestBody"]["content"]["application/json"];

export interface AccountsClient {
  list(): Promise<AccountsUser[]>;
  create(body: AccountInput, key: string): Promise<{ user: AccountsUser; temporary_password: string | null }>;
  update(id: string, body: AccountRevision, key: string): Promise<{ user: AccountsUser }>;
  setLocked(id: string, lock: boolean, key: string): Promise<{ user: AccountsUser }>;
}

export function accountsClient(scope: AccountsScope, api: ApiClient, adminApi: AdminApiClient): AccountsClient {
  if (scope === "admin") {
    return {
      list: async () => (await unwrap(adminApi.GET("/v1/accounts/htx-users"))).items,
      create: (body, key) => unwrap(adminApi.POST("/v1/accounts/htx-users", { params: { header: { "Idempotency-Key": key } }, body })),
      update: (id, body, key) => unwrap(adminApi.POST("/v1/accounts/htx-users/{id}/revisions", { params: { path: { id }, header: { "Idempotency-Key": key } }, body })),
      setLocked: (id, lock, key) =>
        unwrap(adminApi.POST(lock ? "/v1/accounts/htx-users/{id}/lock" : "/v1/accounts/htx-users/{id}/unlock", { params: { path: { id }, header: { "Idempotency-Key": key } } })),
    };
  }
  return {
    list: async () => (await unwrap(api.GET("/v1/htx-accounts"))).items as AccountsUser[],
    create: async (body, key) => {
      const res = await unwrap(api.POST("/v1/htx-accounts", {
        params: { header: { "Idempotency-Key": key } },
        body: { username: body.username, full_name: body.full_name, phone: body.phone, email: body.email || undefined, cooperative_ref: body.cooperative_ref ?? "", roles: ["htx_member"] },
      }));
      return { user: res.user as AccountsUser, temporary_password: res.temporary_password };
    },
    update: async (id, body, key) => ({ user: (await unwrap(api.POST("/v1/htx-accounts/{id}/revisions", { params: { path: { id }, header: { "Idempotency-Key": key } }, body }))).user as AccountsUser }),
    setLocked: async (id, lock, key) =>
      ({ user: (await unwrap(api.POST(lock ? "/v1/htx-accounts/{id}/lock" : "/v1/htx-accounts/{id}/unlock", { params: { path: { id }, header: { "Idempotency-Key": key } } }))).user as AccountsUser }),
  };
}
