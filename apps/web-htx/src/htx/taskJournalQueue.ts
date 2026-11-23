import { unwrap, type ApiClient } from "@/api/client";
import { uuidv7 } from "@/api/idempotency";
import type { components } from "@/api/generated/application";
import type { Principal } from "@/auth/principal";
import { matrixAllows } from "@/auth/permissions";
import { ApiProblem } from "@/api/problem";
import { uploadTaskPhoto, type TaskPhoto } from "./taskPhoto";

type Input = components["schemas"]["HouseholdTaskDetailInput"];
type QueueState = { owner: string; key: CryptoKey; device: string; seq: number };
type Stored = { id: string; owner: string; iv: Uint8Array<ArrayBuffer>; ciphertext: ArrayBuffer };
export type PendingTask = { id: string; device: string; seq: number; eventTime: string; coop: string; household: string; season: string; input: Input; photos: TaskPhoto[]; error?: string };

function owner(p: Principal): string {
  if (!p.subject || !p.tenantId || !matrixAllows(p, "KHMV-FN-05", "C")) throw new Error("Phiên hiện tại không được ghi nhật ký.");
  return `${p.tenantId}:${p.subject}:${p.orgRef ?? "all"}`;
}
function openDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const r = indexedDB.open("htx-native-task-journals", 1);
    r.onupgradeneeded = () => { r.result.createObjectStore("state", { keyPath: "owner" }); r.result.createObjectStore("records", { keyPath: "id" }); r.result.createObjectStore("references", { keyPath: "id" }); };
    r.onsuccess = () => resolve(r.result); r.onerror = () => reject(r.error);
  });
}
function get<T>(db: IDBDatabase, store: string, id: string): Promise<T | undefined> {
  return new Promise((resolve, reject) => { const r = db.transaction(store).objectStore(store).get(id); r.onsuccess = () => resolve(r.result as T | undefined); r.onerror = () => reject(r.error); });
}
async function state(db: IDBDatabase, scope: string, allocate: boolean): Promise<QueueState> {
  const candidate = await crypto.subtle.generateKey({ name: "AES-GCM", length: 256 }, false, ["encrypt", "decrypt"]);
  return new Promise((resolve, reject) => {
    const tx = db.transaction("state", "readwrite"), store = tx.objectStore("state"), request = store.get(scope);
    let result: QueueState;
    request.onsuccess = () => { result = request.result ?? { owner: scope, key: candidate, device: uuidv7(), seq: 0 }; if (allocate) result.seq++; store.put(result); };
    tx.oncomplete = () => resolve(result); tx.onabort = () => reject(tx.error ?? new Error("Không lưu được trạng thái hàng đợi."));
  });
}
function mutate(db: IDBDatabase, value: Stored | string, store = "records", insertOnly = false): Promise<void> {
  return new Promise((resolve, reject) => { const tx = db.transaction(store, "readwrite"), s = tx.objectStore(store); if (typeof value === "string") s.delete(value); else if (insertOnly) s.add(value); else s.put(value); tx.oncomplete = () => resolve(); tx.onabort = () => reject(tx.error ?? new Error("Không lưu được nhật ký trên máy.")); });
}

function purgeOwner(db: IDBDatabase, scope: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const tx = db.transaction(["state", "records", "references"], "readwrite");
    tx.objectStore("state").delete(scope);
    for (const name of ["records", "references"]) {
      const request = tx.objectStore(name).openCursor();
      request.onsuccess = () => { const cursor = request.result; if (!cursor) return; if ((cursor.value as Stored).owner === scope) cursor.delete(); cursor.continue(); };
    }
    tx.oncomplete = () => resolve(); tx.onabort = () => reject(tx.error ?? new Error("Không xóa được dữ liệu thiết bị đã thu hồi."));
  });
}

/** Cache previously authorized reads; denials purge that entry. Encryption and
 * partitioning are the same as the native queue, including current HTX. */
export async function cachedTaskRead<T>(p: Principal, name: string, load: () => Promise<T>): Promise<T> {
  if (typeof indexedDB === "undefined") return load();
  const scope = owner(p), db = await openDB(), id = `${scope}:${name}`;
  try {
    const s = await state(db, scope, false), aad = new TextEncoder().encode(id);
    try {
      const value = await load(), iv = crypto.getRandomValues(new Uint8Array(12)), plain = new TextEncoder().encode(JSON.stringify(value));
      try { await mutate(db, { id, owner: scope, iv, ciphertext: await crypto.subtle.encrypt({ name: "AES-GCM", iv, additionalData: aad }, s.key, plain) }, "references"); }
      finally { plain.fill(0); }
      return value;
    } catch (e) {
      if (e instanceof ApiProblem && e.status > 0 && e.status < 500) { await mutate(db, id, "references"); throw e; }
      if (!(e instanceof TypeError) && !(e instanceof ApiProblem && (e.status === 0 || e.status >= 500))) throw e;
      const row = await get<Stored>(db, "references", id); if (!row) throw e;
      const plain = new Uint8Array(await crypto.subtle.decrypt({ name: "AES-GCM", iv: row.iv, additionalData: aad }, s.key, row.ciphertext));
      try { return JSON.parse(new TextDecoder().decode(plain)) as T; } finally { plain.fill(0); }
    }
  } finally { db.close(); }
}
async function seal(item: PendingTask, s: QueueState): Promise<Stored> {
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const plain = new TextEncoder().encode(JSON.stringify({ ...item, photos: item.photos.map((p) => ({ ...p, ciphertext: Array.from(p.ciphertext) })) }));
  try { return { id: item.id, owner: s.owner, iv, ciphertext: await crypto.subtle.encrypt({ name: "AES-GCM", iv, additionalData: new TextEncoder().encode(`${s.owner}:${item.id}`) }, s.key, plain) }; }
  finally { plain.fill(0); }
}
async function unseal(row: Stored, s: QueueState): Promise<PendingTask> {
  const plain = new Uint8Array(await crypto.subtle.decrypt({ name: "AES-GCM", iv: row.iv, additionalData: new TextEncoder().encode(`${s.owner}:${row.id}`) }, s.key, row.ciphertext));
  try {
    const parsed = JSON.parse(new TextDecoder().decode(plain)) as Omit<PendingTask, "photos"> & { photos: (Omit<TaskPhoto, "ciphertext"> & { ciphertext: number[] })[] };
    return { ...parsed, photos: parsed.photos.map((p) => ({ ...p, ciphertext: new Uint8Array(p.ciphertext) })) };
  } finally { plain.fill(0); }
}

export async function enqueueTask(p: Principal, item: Omit<PendingTask, "seq" | "device" | "eventTime">): Promise<void> {
  const scope = owner(p), db = await openDB();
  try {
    const unchanged = async (row: Stored) => {
      if (row.owner !== scope) throw new Error("Thao tác thuộc tài khoản khác.");
      const s = await get<QueueState>(db, "state", scope); if (!s) throw new Error("Thiếu khóa nhật ký trên máy.");
      const previous = await unseal(row, s);
      const business = (r: typeof item) => JSON.stringify({ coop: r.coop, household: r.household, season: r.season, input: r.input, photos: r.photos.map((p) => ({ ref: p.ref, meta: p.meta, ciphertext: Array.from(p.ciphertext) })) });
      if (business(previous) !== business(item)) throw new Error("Nội dung thao tác đã thay đổi; cần một khóa mới.");
    };
    const existing = await get<Stored>(db, "records", item.id);
    if (existing) { await unchanged(existing); return; }
    const s = await state(db, scope, true);
    const row = await seal({ ...item, seq: s.seq, device: s.device, eventTime: new Date().toISOString() }, s);
    try { await mutate(db, row, "records", true); }
    catch (e) {
      if (!(e && typeof e === "object" && "name" in e && e.name === "ConstraintError")) throw e;
      const first = await get<Stored>(db, "records", item.id); if (!first) throw e;
      await unchanged(first);
    }
  } finally { db.close(); }
}

export async function queuedTasks(p: Principal, coop: string, household: string, season: string): Promise<PendingTask[]> {
  const scope = owner(p), db = await openDB();
  try {
    const s = await get<QueueState>(db, "state", scope); if (!s) return [];
    const rows = await new Promise<Stored[]>((resolve, reject) => { const r = db.transaction("records").objectStore("records").getAll(); r.onsuccess = () => resolve(r.result as Stored[]); r.onerror = () => reject(r.error); });
    const items = await Promise.all(rows.filter((r) => r.owner === scope).map((r) => unseal(r, s)));
    return items.filter((r) => r.coop === coop && r.household === household && r.season === season).sort((a, b) => a.seq - b.seq);
  } finally { db.close(); }
}

/** AH-114 AC4: người dùng xóa bản ghi đã bị máy chủ từ chối (vd vụ đã đóng). Bản chờ gửi không xóa được. */
export async function discardTask(p: Principal, id: string): Promise<boolean> {
  const scope = owner(p), db = await openDB();
  try {
    const s = await get<QueueState>(db, "state", scope); if (!s) return false;
    const row = await get<Stored>(db, "records", id);
    if (!row || row.owner !== scope || !(await unseal(row, s)).error) return false;
    await mutate(db, id);
    return true;
  } finally { db.close(); }
}

/** Lý do máy chủ từ chối nhật ký việc → lời dễ hiểu (AH-114 AC4, AC7). */
export function taskRejectMessage(code: string): string {
  switch (code) {
    case "cropcycle.task_season_state": return "Chưa đồng bộ: vụ đã đóng";
    case "cropcycle.season_not_found": return "Chưa đồng bộ: vụ hoặc nông hộ không thuộc phạm vi của bạn";
    case "cropcycle.task_already_resolved": return "Chưa đồng bộ: việc đã được ghi xong hoặc bỏ qua (có thể từ máy khác)";
    case "cropcycle.task_season_changed": return "Chưa đồng bộ: quy trình của vụ đã đổi — tải lại checklist rồi ghi lại";
    case "cropcycle.task_stage_locked": return "Chưa đồng bộ: vụ đã thu hoạch, chỉ ghi được việc của giai đoạn cuối";
    case "cropcycle.read_only": return "Chưa đồng bộ: vai của bạn chỉ được xem";
    case "validation_failed": return "Chưa đồng bộ: dữ liệu chưa hợp lệ";
    default: return `Chưa đồng bộ: ${code}`;
  }
}

export async function flushTasks(api: ApiClient, p: Principal, items: PendingTask[]): Promise<void> {
  const scope = owner(p), db = await openDB();
  try {
    const s = await get<QueueState>(db, "state", scope); if (!s) return;
    for (const item of items) {
      if (item.device !== s.device) throw new Error("Thiết bị không thuộc phiên ghi nhật ký.");
      await unwrap(api.POST("/v1/devices", { params: { header: { "Idempotency-Key": `reg:${item.device}` } }, body: { device_id: item.device, platform: "web", app_version: "htx-task-1" } }));
      const files: string[] = []; for (const photo of item.photos) files.push(await uploadTaskPhoto(api, item.device, photo));
      const input = item.input;
      const result = await unwrap(api.POST("/v1/sync/push", { params: { header: { "X-Device-Id": item.device, "Idempotency-Key": uuidv7() } }, body: { device_id: item.device, records: [{ id: item.id, idem_key: `dev:${item.device}:${item.seq}`, entity: "household_task_record", op: "create", event_time: item.eventTime, payload: { ...input, file_refs: files, recorded_by: p.subject, tenant_id: p.tenantId, cooperative_ref: item.coop, household_ref: item.household, season_ref: item.season } }] } }));
      const outcome = result.results.find((r) => r.id === item.id);
      if (outcome?.status === "accepted" || outcome?.status === "duplicate") await mutate(db, item.id);
      else { await mutate(db, await seal({ ...item, error: outcome?.code ?? "Không nhận được xác nhận của máy chủ" }, s)); throw new Error(outcome?.code ?? "Chưa đồng bộ được nhật ký."); }
    }
  } catch (e) {
    if (e instanceof ApiProblem && e.code === "sync.device_revoked") await purgeOwner(db, scope);
    throw e;
  } finally { db.close(); }
}
