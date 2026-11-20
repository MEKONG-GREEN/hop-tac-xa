import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { createAdminApiClient } from "@/api/client";
import { MOCK_PERSONAS, mockToken } from "@/api/mock/personas";
import { AuthContextForTests, type AuthContextValue } from "@/auth/AuthProvider";
import { buildPrincipal } from "@/auth/principal";
import { StrawFactorsScreen } from "./StrawFactorsScreen";

function view(role:string,fetch:typeof globalThis.fetch){
 const p=MOCK_PERSONAS.find((p)=>p.me.roles.includes(role))!;
 const principal=buildPrincipal({sub:p.me.actor_ref,name:p.displayName,amr:p.amr,realm_access:{roles:p.me.roles}},p.me);
 const auth={status:"authenticated",principal,adminApi:createAdminApiClient({baseUrl:"https://admin.test",getAccessToken:()=>mockToken(p.id),fetch})} as unknown as AuthContextValue;
 return <AuthContextForTests.Provider value={auth}><StrawFactorsScreen/></AuthContextForTests.Provider>;
}
const list={items:[{variety_code:"OM18",variety_name:"OM18",active:true,configured:false,percentage:"35",revision:0,effective_at:null}]};
const json=(body:unknown,status=200)=>new Response(JSON.stringify(body),{status,headers:{"Content-Type":"application/json"}});

describe("AH-282: hệ số theo giống",()=>{
 it.each(["htx_member","coop_manager","kn_officer"])("%s không có màn hệ số hoặc request dữ liệu",(role)=>{
  const fetch=vi.fn<typeof globalThis.fetch>();render(view(role,fetch));expect(screen.queryByRole("button",{name:"Thêm hệ số"})).toBeNull();expect(fetch).not.toHaveBeenCalled();
 });
 it("mặc định 35%; chặn 120 và gửi decimal string cùng Idempotency-Key",async()=>{
  const posts:Request[]=[];const fetch=vi.fn<typeof globalThis.fetch>().mockImplementation(async(input,init)=>{
   const req=new Request(input,init);if(req.method==="GET")return json(list);posts.push(req);return json({id:"event",revision:1},201);
  });
  render(view("platform_admin",fetch));await screen.findByText("35%");
  fireEvent.click(screen.getByRole("button",{name:"Thêm hệ số"}));
  fireEvent.change(screen.getByLabelText("Giống *"),{target:{value:"OM18"}});
  fireEvent.change(screen.getByLabelText("Hệ số (%) *"),{target:{value:"120"}});fireEvent.click(screen.getByRole("button",{name:"Lưu hệ số"}));
  expect(await screen.findByText("Hệ số từ 0 đến 100%")).toBeTruthy();expect(posts).toHaveLength(0);
  fireEvent.change(screen.getByLabelText("Hệ số (%) *"),{target:{value:"35.5555"}});fireEvent.click(screen.getByRole("button",{name:"Lưu hệ số"}));
  await screen.findByText("Đã lưu hệ số rơm.");expect(posts).toHaveLength(1);expect(posts[0]!.headers.get("Idempotency-Key")).toBeTruthy();expect(await posts[0]!.json()).toEqual({variety_code:"OM18",percentage:"35.5555"});
 });
 it("mất phản hồi giữ khóa và payload để thử lại",async()=>{
  const posts:Request[]=[];const fetch=vi.fn<typeof globalThis.fetch>().mockImplementation(async(input,init)=>{
   const req=new Request(input,init);if(req.method==="GET")return json(list);posts.push(req);
   if(posts.length===1)return new Response(JSON.stringify({status:503,code:"unavailable",title:"Dịch vụ tạm ngừng"}),{status:503,headers:{"Content-Type":"application/problem+json"}});
   return json({id:"event",revision:1},201);
  });
  render(view("platform_admin",fetch));await screen.findByText("35%");fireEvent.click(screen.getByRole("button",{name:"Thêm hệ số"}));
  fireEvent.change(screen.getByLabelText("Giống *"),{target:{value:"OM18"}});fireEvent.change(screen.getByLabelText("Hệ số (%) *"),{target:{value:"30"}});
  const save=screen.getByRole("button",{name:"Lưu hệ số"});fireEvent.click(save);fireEvent.click(save);await screen.findByText("Dịch vụ tạm ngừng");expect(posts).toHaveLength(1);
  fireEvent.click(screen.getByRole("button",{name:"Lưu hệ số"}));await waitFor(()=>expect(posts).toHaveLength(2));
  expect(posts[1]!.headers.get("Idempotency-Key")).toBe(posts[0]!.headers.get("Idempotency-Key"));expect(await posts[1]!.json()).toEqual(await posts[0]!.json());
 });
});
