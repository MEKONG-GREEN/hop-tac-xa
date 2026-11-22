import { beforeEach, expect, it } from "vitest";
import { createApiClient, unwrap } from "@/api/client";
import { COOPERATIVES } from "./data";
import { mockFetchNoDelay } from "./handler";
import { resetNativeSharingMock, seedNativeSharing } from "./native-sharing";
import { MOCK_PERSONAS, mockToken } from "./personas";

// FN-03/AC-01 (AH-284): mock khớp backend — HTX của Đại diện đứng đầu, bộ lọc HTX của tôi / Được chia sẻ.
beforeEach(resetNativeSharingMock);
const own = () => MOCK_PERSONAS.find((p) => p.id === "htx")!.me.org!.ref;
const client = (id: string) => createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(id), fetch: mockFetchNoDelay });
const list = async (id: string, relation?: "own" | "shared") =>
  (await unwrap(client(id).GET("/v1/cooperatives", { params: { query: { relation, limit: 200 } } }))).items.map((c) => c.ref);

it("puts the Representative's own HTX first and filters by relation", async () => {
  const shared = COOPERATIVES.filter((c) => c.api.active && c.api.ref !== own()).slice(0, 2).map((c) => c.api.ref);
  for (const ref of shared) seedNativeSharing(own(), ref, "approved");
  const all = await list("htx");
  expect(all[0]).toBe(own());
  expect([...all.slice(1)].sort()).toEqual([...shared].sort());
  expect(await list("htx", "own")).toEqual([own()]);
  expect((await list("htx", "shared")).sort()).toEqual([...shared].sort());
});

it("gives a Member no shared HTX", async () => {
  seedNativeSharing(own(), COOPERATIVES.find((c) => c.api.active && c.api.ref !== own())!.api.ref, "approved");
  expect(await list("htx-member", "shared")).toEqual([]);
  expect(await list("htx-member")).toEqual([MOCK_PERSONAS.find((p) => p.id === "htx-member")!.me.org!.ref]);
});
