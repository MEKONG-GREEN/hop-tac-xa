import { describe, expect, it } from "vitest";
import { createApiClient, unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { htxCoop } from "./data";
import { mockFetchNoDelay } from "./handler";
import { MOCK_PERSONAS, mockToken } from "./personas";

describe("AH-59 calendar mock contract",()=>{
 it("Rep phát/sửa/hủy, Member chỉ đọc, phát lại cùng key không tạo lịch trùng",async()=>{
  const client=(p:string)=>createApiClient({baseUrl:"https://api.test",getAccessToken:()=>mockToken(p),fetch:mockFetchNoDelay});
  const manager=client("htx"),member=client("htx-member"),coop=htxCoop().api.ref;
  const actor=MOCK_PERSONAS.find((p)=>p.id==="htx-member")!.me.actor_ref;
  const body={crop_code:"LUA",season_code:"DX",season_year:2026,sow_from:"2026-12-25",sow_to:"2027-01-05",recipients:[actor]};
  const key=newIdempotencyKey();const send=()=>unwrap(manager.POST("/v1/cooperatives/{ref}/season-calendars",{params:{path:{ref:coop},header:{"Idempotency-Key":key}},body}));
  const c=await send();expect((await send()).calendar_ref).toBe(c.calendar_ref);
  const detail=await unwrap(member.GET("/v1/cropcycle/calendars/{calendar_ref}",{params:{path:{calendar_ref:c.calendar_ref}}}));
  expect(detail.can_manage).toBe(false);expect(detail.recipients).toBeUndefined();
  const denied=await member.POST("/v1/cooperatives/{ref}/season-calendars",{params:{path:{ref:coop},header:{"Idempotency-Key":newIdempotencyKey()}},body});expect(denied.response.status).toBe(403);
  const revised=await unwrap(manager.POST("/v1/cooperatives/{ref}/season-calendars/{calendar_ref}/revisions",{params:{path:{ref:coop,calendar_ref:c.calendar_ref},header:{"Idempotency-Key":newIdempotencyKey()}},body:{...body,expected_event_id:c.current_event_id,sow_from:"2026-12-26"}}));
  expect(revised.version).toBe(c.version+1);
  await unwrap(manager.POST("/v1/cooperatives/{ref}/season-calendars/{calendar_ref}/cancel",{params:{path:{ref:coop,calendar_ref:c.calendar_ref},header:{"Idempotency-Key":newIdempotencyKey()}},body:{expected_event_id:revised.current_event_id}}));
  const gone=await member.GET("/v1/cropcycle/calendars/{calendar_ref}",{params:{path:{calendar_ref:c.calendar_ref}}});expect(gone.response.status).toBe(404);
  const page=await unwrap(member.GET("/v1/cooperatives/{ref}/season-calendars",{params:{path:{ref:coop}}}));expect(page.items.some((v)=>v.calendar_ref===c.calendar_ref)).toBe(false);
 });
});
