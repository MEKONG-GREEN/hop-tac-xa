import { unwrap, type ApiClient, type AdminApiClient } from "@/api/client";
import type { components as App } from "@/api/generated/application";
import type { components as Admin } from "@/api/generated/admin";
import { hasRole, ROLES, type Principal } from "@/auth/principal";

export type HTXProfile = App["schemas"]["HTXProfile"];
export type HTXProfileInput = Admin["schemas"]["HTXProfileInput"];
export type HTXDeletionCheck = Admin["schemas"]["HTXDeletionCheck"];

export function checkHTXDeletion(api: AdminApiClient, ref: string): Promise<HTXDeletionCheck> {
  return unwrap(api.GET("/v1/cooperatives/{ref}/deletion", { params: { path: { ref } } }));
}

export function deleteHTX(api: AdminApiClient, ref: string, revision: number, key: string) {
  return unwrap(api.POST("/v1/cooperatives/{ref}/deletions", {
    params: { path: { ref }, header: { "Idempotency-Key": key } }, body: { expected_revision: revision },
  }));
}

export function canEditHTXProfile(principal: Principal | null, ref?: string): boolean {
  return hasRole(principal, ROLES.platformAdmin) || Boolean(ref && principal?.orgRef === ref && hasRole(principal, ROLES.coopManager));
}

export function getHTXProfile(api: ApiClient, ref: string): Promise<HTXProfile> {
  return unwrap(api.GET("/v1/cooperatives/{ref}/profile", { params: { path: { ref } } }));
}

export function createHTXProfile(api: AdminApiClient, body: HTXProfileInput, key: string) {
  return unwrap(api.POST("/v1/cooperative-profiles", { params: { header: { "Idempotency-Key": key } }, body }));
}

export function updateHTXProfile(api: ApiClient, profile: HTXProfile, body: HTXProfileInput, key: string) {
  return unwrap(api.POST("/v1/cooperatives/{ref}/profile/revisions", {
    params: { path: { ref: profile.ref }, header: { "Idempotency-Key": key } }, body: { ...body, expected_revision: profile.revision },
  }));
}
