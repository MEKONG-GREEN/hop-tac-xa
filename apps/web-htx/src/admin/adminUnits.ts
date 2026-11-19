import type { AdminApiClient } from "@/api/client";
import { unwrap } from "@/api/client";
import type { components } from "@/api/generated/admin";

export type ManagedUnit = components["schemas"]["ManagedAdminUnit"];

export async function listManagedUnits(api: AdminApiClient, parent: string): Promise<ManagedUnit[]> {
  const items: ManagedUnit[] = [];
  const seen = new Set<string>();
  let after: string | undefined;
  do {
    const page = await unwrap(api.GET("/v1/masterdata/admin-unit-profiles", { params: { query: { parent, after, limit: 200 } } }));
    items.push(...page.items);
    after = page.next_cursor ?? undefined;
    if (after && seen.has(after)) throw new Error("Không tải xong danh mục. Vui lòng thử lại.");
    if (after) seen.add(after);
  } while (after);
  return items;
}
