import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { collectPages } from "@/api/pagination";
import { useAsync } from "@/lib/useAsync";

type GPS = components["schemas"]["CropGPS"];

/** Danh mục cây, giống, mùa vụ và SOP đã phát hành để mở vụ theo hộ (AH-61, AH-68). */
export function useSeasonMetadata(api: ApiClient) {
  return useAsync(async () => {
    const catalog = (name: "crop" | "rice_variety" | "season") => collectPages((cursor) => unwrap(api.GET("/v1/masterdata/catalogs/{catalog}", { params: { path: { catalog: name }, query: { cursor, limit: 200 } } })));
    const [crops, varieties, seasons, templates, bindings] = await Promise.all([
      catalog("crop"), catalog("rice_variety"), catalog("season"),
      collectPages((cursor) => unwrap(api.GET("/v1/cropcycle/sops", { params: { query: { status: "published", cursor, limit: 200 } } }))),
      unwrap(api.GET("/v1/cropcycle/variety-bindings")),
    ]);
    return { crops, varieties, seasons, templates, bindings: bindings.items };
  }, [api]);
}

export type SeasonMetadata = NonNullable<ReturnType<typeof useSeasonMetadata>["data"]>;

/** Phiên bản SOP đang áp dụng cho loại cây/giống trong HTX: ưu tiên SOP của giống (hoặc gắn với giống). */
export function useSopChoices(api: ApiClient, metadata: SeasonMetadata | null | undefined, coopRef: string, crop: string, variety: string) {
  return useAsync(async () => {
    const eligible = (metadata?.templates ?? []).filter((t) => t.crop === crop && (!t.variety_code || t.variety_code === variety) && (t.scope === "system" || t.cooperative_ref === coopRef));
    const bound = metadata?.bindings.find((b) => b.variety_code === variety)?.template_ref;
    const specific = eligible.filter((t) => t.variety_code === variety || t.ref === bound);
    return Promise.all((specific.length ? specific : eligible).map(async (t) => {
      const d = await unwrap(api.GET("/v1/cropcycle/sops/{ref}", { params: { path: { ref: t.ref } } }));
      const v = d.versions.find((v) => v.state === "active" && v.version_no === t.published_version);
      return v ? { id: v.id, label: `${t.name} · bản ${v.version_no}`, anchor: t.anchor_name } : null;
    })).then((rows) => rows.filter((r) => r !== null));
  }, [api, metadata, coopRef, crop, variety]);
}

/** Diện tích vụ: số dương, tối đa hai chữ số thập phân, không làm tròn. */
export function householdAreaError(raw: string): string | null {
  if (!/^(0|[1-9][0-9]{0,9})(\.[0-9]{1,2})?$/.test(raw) || !/[1-9]/.test(raw)) return "Diện tích phải lớn hơn 0 và có tối đa hai chữ số thập phân.";
  return null;
}

/** Vị trí lúc lưu; từ chối hoặc không có thiết bị vẫn mở vụ được (ghi "Thiếu GPS"). */
export async function requestGPS(): Promise<GPS> {
  if (!navigator.geolocation) return { source: "no_device" };
  return new Promise((resolve) => { try { navigator.geolocation.getCurrentPosition(
    (p) => resolve({ lon: p.coords.longitude.toFixed(7), lat: p.coords.latitude.toFixed(7), accuracy_m: p.coords.accuracy.toFixed(2), source: "device" }),
    () => resolve({ source: "denied" }),
    { enableHighAccuracy: true, timeout: 15000, maximumAge: 0 },
  ); } catch { resolve({ source: "denied" }); } });
}
