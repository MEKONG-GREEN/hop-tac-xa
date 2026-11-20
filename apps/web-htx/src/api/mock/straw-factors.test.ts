import { describe, expect, it } from "vitest";
import { createAdminApiClient, unwrap } from "../client";
import { mockAdminFetchNoDelay } from "./handler";
import { MOCK_PERSONAS, mockToken } from "./personas";

function api(role:string){const p=MOCK_PERSONAS.find((p)=>p.me.roles.includes(role))!;return createAdminApiClient({baseUrl:"http://mock",getAccessToken:()=>mockToken(p.id),fetch:mockAdminFetchNoDelay})}
let n=0;const headers=()=>({"Idempotency-Key":`ah282-mock-${++n}`});
describe("AH-282: mock khớp hợp đồng",()=>{
 it("lịch sử hệ số bảo vệ giống chưa từng có vụ khỏi xóa cứng",async()=>{
  const admin=api("platform_admin");
  const variety=await unwrap(admin.POST("/v1/masterdata/catalogs/{catalog}/items",{params:{path:{catalog:"rice_variety"},header:headers()},body:{name:"Giống thử hệ số AH282",attrs:{crop_code:"LUA",growth_days:"90"}}}));
  const code=variety.code;
  const first=await unwrap(admin.POST("/v1/cropcycle/straw-factors",{params:{header:headers()},body:{variety_code:code,percentage:"30"}}));
  expect(first.percentage).toBe("30");
  await unwrap(admin.POST("/v1/cropcycle/straw-factors/{variety_code}/revisions",{params:{path:{variety_code:code},header:headers()},body:{percentage:"40",expected_revision:1}}));
  const history=await unwrap(admin.GET("/v1/cropcycle/straw-factors/{variety_code}/history",{params:{path:{variety_code:code}}}));
  expect(history.items).toHaveLength(2);expect(history.items[0]?.previous_percentage).toBe("30");
  const deleted=await unwrap(admin.POST("/v1/masterdata/catalogs/{catalog}/items/{code}/delete",{params:{path:{catalog:"rice_variety",code},header:headers()}}));
  expect(deleted.outcome).toBe("hidden");
  const list=await unwrap(admin.GET("/v1/cropcycle/straw-factors"));expect(list.items.find((r)=>r.variety_code===code)).toMatchObject({active:false,configured:true,percentage:"40"});
 });
 it("Đại diện/Member không gọi được API hệ số",async()=>{
  for(const role of ["coop_manager","htx_member"]){const client=api(role);expect((await client.GET("/v1/cropcycle/straw-factors")).response.status).toBe(403);expect((await client.POST("/v1/cropcycle/straw-factors",{params:{header:headers()},body:{variety_code:"OM18",percentage:"35"}})).response.status).toBe(403)}
 });
});
