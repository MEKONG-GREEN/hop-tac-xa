import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { MOCK_PERSONAS } from "@/api/mock/personas";
import { AuthContextForTests, type AuthContextValue } from "@/auth/AuthProvider";
import { buildPrincipal } from "@/auth/principal";
import { SeasonCalendars } from "./SeasonCalendars";
import { calendarDraftError, type SeasonCalendar } from "./seasonCalendarModel";

const CAL="01920000-0000-7000-8000-00000000ca11",EVENT="01920000-0000-7000-8000-00000000ca12";
function setup(persona="htx",failOnce=false,existing=false){
 const p=MOCK_PERSONAS.find((p)=>p.id===persona)!;
 const member=MOCK_PERSONAS.find((p)=>p.id==="htx-member")!;
 const principal=buildPrincipal({sub:p.me.actor_ref,name:p.displayName,amr:p.amr,realm_access:{roles:p.me.roles}},p.me);
 const writes:{path:string;key:string|null;body:Record<string,unknown>}[]=[];
 const manage=persona==="htx";
 let calendar:SeasonCalendar|null=existing?{calendar_ref:CAL,cooperative_ref:p.me.org!.ref,current_event_id:EVENT,version:1,status:"published",crop_code:"LUA",season_code:"DX",season_year:2026,sow_from:"2026-12-25",sow_to:"2027-01-05",can_manage:manage,recipients:manage?[member.me.actor_ref]:undefined,updated_at:"2026-11-19T00:00:00Z"}:null;
 const api=createApiClient({baseUrl:"https://api.test",getAccessToken:()=>"test",fetch:async(input)=>{
  const req=input as Request;const path=new URL(req.url).pathname;
  if(req.method==="POST"){
   const body=await req.json() as Record<string,unknown>;writes.push({path,key:req.headers.get("Idempotency-Key"),body});
   if(failOnce&&writes.length===1)throw new TypeError("offline");
   calendar={calendar_ref:CAL,cooperative_ref:p.me.org!.ref,current_event_id:EVENT,version:1,status:path.endsWith("cancel")?"cancelled":"published",crop_code:"LUA",season_code:"DX",season_year:2026,sow_from:"2026-12-25",sow_to:"2027-01-05",can_manage:manage,updated_at:"2026-11-19T00:00:00Z",...(path.endsWith("cancel")?calendar:body)} as SeasonCalendar;
   calendar.status=path.endsWith("cancel")?"cancelled":"published";
   return Response.json(calendar,{status:201});
  }
  if(path.includes("/masterdata/catalogs/"))return Response.json({items:[{code:path.endsWith("crop")?"LUA":"DX",name:path.endsWith("crop")?"Lúa":"Đông xuân",active:true,attrs:{}}],next_cursor:null});
  if(path.endsWith("season-calendar-recipients"))return Response.json({items:[{actor_ref:member.me.actor_ref,label:"Thành viên được chọn"}],next_offset:0});
  if(path.includes("/cropcycle/calendars/"))return calendar?Response.json(calendar):Response.json({status:404,code:"cropcycle.calendar_not_found",title:"Lịch đã hủy"},{status:404});
  if(path.endsWith("season-calendars"))return Response.json({items:calendar?[calendar]:[],next_cursor:null});
  return Response.json({items:[],next_cursor:null});
 }});
 render(<AuthContextForTests.Provider value={{api,principal,status:"authenticated"} as AuthContextValue}><SeasonCalendars initialRef={existing?CAL:""}/></AuthContextForTests.Provider>);
 return {writes};
}
async function fill(){
 fireEvent.click(await screen.findByRole("button",{name:"Lập lịch"}));
 await screen.findByRole("checkbox",{name:"Thành viên được chọn"});
 fireEvent.change(screen.getByLabelText("Loại cây"),{target:{value:"LUA"}});
 fireEvent.change(screen.getByLabelText("Mùa vụ"),{target:{value:"DX"}});
 fireEvent.change(screen.getByLabelText("Năm"),{target:{value:"2026"}});
 fireEvent.change(screen.getByLabelText("Xuống giống từ"),{target:{value:"2026-12-25"}});
 fireEvent.change(screen.getByLabelText("Đến ngày"),{target:{value:"2027-01-05"}});
 fireEvent.click(screen.getByRole("checkbox",{name:"Thành viên được chọn"}));
}
describe("AH-59 lịch thời vụ",()=>{
 it("khung qua năm được phép và ngày đảo bị chặn",()=>{
  const draft={crop_code:"LUA",season_code:"DX",season_year:2026,sow_from:"2026-12-25",sow_to:"2027-01-05",recipients:[CAL]};
  expect(calendarDraftError(draft)).toBeNull();expect(calendarDraftError({...draft,sow_to:"2026-12-24"})).toContain("Ngày từ");
 });
 it("phát lịch giữ key khi mạng lỗi, không có giống/SOP/thu hoạch",async()=>{
  const{writes}=setup("htx",true);await fill();
  expect(screen.queryByLabelText("Giống")).toBeNull();expect(screen.queryByLabelText(/SOP/)).toBeNull();expect(screen.queryByLabelText(/thu hoạch/)).toBeNull();
  fireEvent.click(screen.getByRole("button",{name:"Phát lịch"}));await waitFor(()=>expect(writes).toHaveLength(1));
  await waitFor(()=>expect(screen.getByRole("button",{name:"Phát lịch"})).not.toBeDisabled());
  fireEvent.click(screen.getByRole("button",{name:"Phát lịch"}));await waitFor(()=>expect(writes).toHaveLength(2));
  expect(writes[0]!.key).toBeTruthy();expect(writes[1]!.key).toBe(writes[0]!.key);expect(writes[1]!.body).toEqual(writes[0]!.body);
  await screen.findByRole("button",{name:"Sửa lịch"});
 });
 it("Member chỉ đọc và có lối mở vụ theo lịch",async()=>{
  const{writes}=setup("htx-member",false,true);await screen.findByRole("button",{name:"Mở vụ theo lịch"});
  expect(screen.queryByRole("button",{name:"Lập lịch"})).toBeNull();expect(screen.queryByRole("button",{name:"Sửa lịch"})).toBeNull();expect(screen.queryByRole("button",{name:"Hủy lịch"})).toBeNull();expect(writes).toHaveLength(0);
 });
 it("hủy cần xác nhận và gửi đúng phiên bản, không xếp yêu cầu trước xác nhận",async()=>{
  const{writes}=setup("htx",false,true);fireEvent.click(await screen.findByRole("button",{name:"Hủy lịch"}));
  expect(writes).toHaveLength(0);fireEvent.click(screen.getByRole("button",{name:"Xác nhận hủy lịch"}));
  await waitFor(()=>expect(writes).toHaveLength(1));expect(writes[0]!.path).toMatch(/\/cancel$/);expect(writes[0]!.body).toEqual({expected_event_id:EVENT});
  await waitFor(()=>expect(screen.queryByRole("button",{name:"Sửa lịch"})).toBeNull());
 });
});
