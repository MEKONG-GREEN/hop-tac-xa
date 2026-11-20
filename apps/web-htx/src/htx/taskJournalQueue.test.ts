import { webcrypto } from "node:crypto";
import { IDBFactory } from "fake-indexeddb";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { MOCK_PERSONAS } from "@/api/mock/personas";
import { buildPrincipal } from "@/auth/principal";
import { createApiClient } from "@/api/client";
import { cachedTaskRead, enqueueTask, queuedTasks, flushTasks } from "./taskJournalQueue";
import { ApiProblem } from "@/api/problem";
import { actualMaterialTotal } from "./HouseholdTaskDetail";
import { uuidv7 } from "@/api/idempotency";

const persona = MOCK_PERSONAS.find((p) => p.id === "htx-member")!;
const principal = buildPrincipal({ sub: persona.me.actor_ref, amr: persona.amr, realm_access: { roles: persona.me.roles } }, persona.me);
const item = () => ({ id: uuidv7(), coop: principal.orgRef!, household: uuidv7(), season: uuidv7(), photos: [], input: { step_seq: 3, expected_season_event_id: uuidv7(), state: "partial" as const, done_on: "2026-11-20", zero_confirmed: false, channel: "web" as const, materials: [{ material_code: "URE", dose_per_ha: "1.2345" }], file_refs: [], note: "Ghi thực tế riêng", gps: { source: "denied" as const } } });

beforeEach(() => { vi.stubGlobal("indexedDB", new IDBFactory()); vi.stubGlobal("crypto", webcrypto); });

describe("AH-99 Web native journal outbox", () => {
  it("revocation wipes only the revoked account's device data and encryption key", async () => {
    const value = item(), other = { ...principal, subject: uuidv7() }, second = { ...value, id: uuidv7() };
    await enqueueTask(principal, value); await enqueueTask(other, second);
    const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => "test", fetch: async () => Response.json({ type: "", title: "Thu hồi", status: 403, code: "sync.device_revoked" }, { status: 403 }) });
    await expect(flushTasks(api, principal, await queuedTasks(principal, value.coop, value.household, value.season))).rejects.toMatchObject({ code: "sync.device_revoked" });
    expect(await queuedTasks(principal, value.coop, value.household, value.season)).toEqual([]);
    expect(await queuedTasks(other, second.coop, second.household, second.season)).toHaveLength(1);
  });
  it("concurrent confirmation preserves one immutable operation", async () => {
    const value = item(); await Promise.all([enqueueTask(principal, value), enqueueTask(principal, value)]);
    const read = () => queuedTasks(principal, value.coop, value.household, value.season);
    expect(await read()).toHaveLength(1);
    await expect(enqueueTask(principal, { ...value, input: { ...value.input, note: "Thay đổi sau khi lưu" } })).rejects.toThrow("Nội dung thao tác đã thay đổi");
    expect((await read())[0]?.input.note).toBe(value.input.note);
  });
  it("cached reads cannot survive a denial or transfer to another cooperative", async () => {
    const read = () => cachedTaskRead(principal, "photo-key:household", () => Promise.reject(new TypeError("offline")));
    await cachedTaskRead(principal, "photo-key:household", () => Promise.resolve({ public_key: "test public material" }));
    expect(await read()).toEqual({ public_key: "test public material" });
    await expect(cachedTaskRead({ ...principal, orgRef: uuidv7() }, "photo-key:household", () => Promise.reject(new TypeError("offline")))).rejects.toThrow("offline");
    const denial = new ApiProblem({ type: "", title: "forbidden", status: 403, code: "forbidden" });
    await expect(cachedTaskRead(principal, "photo-key:household", () => Promise.reject(denial))).rejects.toBe(denial);
    await expect(read()).rejects.toThrow("offline");
  });
  it("persists encrypted data, separates accounts, preserves exact decimal totals", async () => {
    const value = item(); await enqueueTask(principal, value);
    const rows = await queuedTasks(principal, value.coop, value.household, value.season);
    expect(rows).toHaveLength(1); expect(rows[0]?.input).toEqual(value.input);
    const db = await new Promise<IDBDatabase>((resolve) => { const r = indexedDB.open("htx-native-task-journals", 1); r.onsuccess = () => resolve(r.result); });
    const stored = await new Promise<object>((resolve) => { const r = db.transaction("records").objectStore("records").get(value.id); r.onsuccess = () => resolve(r.result as object); }); db.close();
    expect(JSON.stringify(stored)).not.toContain(value.input.note);
    expect(Object.keys(stored).sort()).toEqual(["ciphertext", "id", "iv", "owner"]);
    expect(await queuedTasks({ ...principal, subject: uuidv7() }, value.coop, value.household, value.season)).toEqual([]);
    await expect(queuedTasks({ ...principal, roles: ["coop_manager"] }, value.coop, value.household, value.season)).rejects.toThrow();
    expect(actualMaterialTotal("1.2345", "0.50")).toBe("0.61725");
    expect(actualMaterialTotal("0", "0.50")).toBe("0");
    expect(actualMaterialTotal("-1", "0.50")).toBeNull();
  });
  it("survives an uncertain network result and replays the same record/device time", async () => {
    const value = item(); await enqueueTask(principal, value);
    const bodies: Record<string, unknown>[] = []; let offline = true;
    const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => "test", fetch: async (input) => {
      const r = input as Request; const body = await r.json() as Record<string, unknown>;
      if (r.url.endsWith("/devices")) return Response.json({}, { status: 201 });
      bodies.push(body); if (offline) throw new TypeError("offline after receipt");
      return Response.json({ results: [{ id: value.id, status: "duplicate" }] });
    } });
    const read = () => queuedTasks(principal, value.coop, value.household, value.season);
    await expect(flushTasks(api, principal, await read())).rejects.toThrow(); expect(await read()).toHaveLength(1);
    offline = false; await flushTasks(api, principal, await read());
    expect(bodies).toHaveLength(2); expect(bodies[0]).toEqual(bodies[1]);
    const record = (bodies[0]?.records as { id: string; payload: Record<string, unknown>; event_time: string }[])[0]!;
    expect(record.id).toBe(value.id); expect(record.payload.recorded_by).toBe(principal.subject); expect(record.payload.tenant_id).toBe(principal.tenantId);
    expect(record.payload.channel).toBe("web"); expect(await read()).toEqual([]);
  });
  it("keeps rejected records visible and allocates monotonically across concurrent writes", async () => {
    const first = item(), second = { ...item(), household: first.household, season: first.season };
    await Promise.all([enqueueTask(principal, first), enqueueTask(principal, second)]);
    const rows = await queuedTasks(principal, first.coop, first.household, first.season);
    expect(rows.map((r) => r.seq)).toEqual([1, 2]); expect(new Set(rows.map((r) => r.device)).size).toBe(1);
    const api = createApiClient({ baseUrl: "https://api.test", getAccessToken: () => "test", fetch: async (input) => (input as Request).url.endsWith("/devices") ? Response.json({}, { status: 201 }) : Response.json({ results: [{ id: rows[0]!.id, status: "rejected", code: "cropcycle.task_season_changed" }] }) });
    await expect(flushTasks(api, principal, rows)).rejects.toThrow("cropcycle.task_season_changed");
    expect((await queuedTasks(principal, first.coop, first.household, first.season))[0]?.error).toBe("cropcycle.task_season_changed");
  });
});
