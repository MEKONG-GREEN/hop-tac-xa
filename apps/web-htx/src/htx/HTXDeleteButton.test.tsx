import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { createAdminApiClient } from "@/api/client";
import { MOCK_PERSONAS } from "@/api/mock/personas";
import { AuthContextForTests, type AuthContextValue } from "@/auth/AuthProvider";
import { buildPrincipal } from "@/auth/principal";
import { HTXDeleteButton } from "./HTXDeleteButton";

const navigation = vi.hoisted(() => ({ replace: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => navigation }));
const REF="01927b3e-0ff1-7000-8000-000000002830";
const json=(body:unknown,status=200)=>new Response(JSON.stringify(body),{status,headers:{"Content-Type":"application/json"}});

function setup(options:{persona?:string;used?:boolean;unavailable?:boolean;retry?:boolean}={}) {
 const person=MOCK_PERSONAS.find(p=>p.id===(options.persona??"setter"))!;
 const principal=buildPrincipal({sub:person.me.actor_ref,name:person.displayName,amr:person.amr,realm_access:{roles:person.me.roles}},person.me);
 const requests:{key:string|null;body:unknown}[]=[];
 const gets:string[]=[];
 const fetch=async(request:Request)=>{
  if(request.method==="GET") {
   gets.push(request.url);
   if(options.unavailable)return json({status:503,code:"party.dependency_unavailable",title:"Chưa kiểm tra đủ dữ liệu"},503);
   return json({ref:REF,code:"HTX283",name:"HTX thử nghiệm",revision:42,households:options.used?3:0,accounts:options.used?2:0,pending_accounts:0,has_sharing:false,has_business:false,can_delete:!options.used});
  }
  requests.push({key:request.headers.get("Idempotency-Key"),body:await request.json()});
  if(options.retry&&requests.length===1)return json({status:503,code:"party.dependency_unavailable",title:"Thử lại thao tác"},503);
  return json({ref:REF,code:"HTX283",revision:43,deleted:true});
 };
 const auth={principal,adminApi:createAdminApiClient({baseUrl:"https://admin.test",getAccessToken:()=>"t",fetch})} as unknown as AuthContextValue;
 render(<AuthContextForTests value={auth}><HTXDeleteButton cooperativeRef={REF}/></AuthContextForTests>);
 return {requests,gets};
}

describe("AH-283 xác nhận xóa HTX",()=>{
 it.each(["htx","htx-member"])("%s không có thao tác xóa và không gọi API",persona=>{
  const {gets,requests}=setup({persona}); expect(screen.queryByRole("button",{name:"Xóa HTX"})).toBeNull(); expect(gets).toEqual([]);expect(requests).toEqual([]);
 });
 it("hiện đúng số hộ/tài khoản và chặn xác nhận HTX đã dùng",async()=>{
  const {requests}=setup({used:true});fireEvent.click(screen.getByRole("button",{name:"Xóa HTX"}));
  expect(await screen.findByText("HTX còn 3 hộ, 2 tài khoản.")).toBeInTheDocument();
  expect(screen.getByRole("button",{name:"Xác nhận xóa HTX"})).toBeDisabled();expect(requests).toEqual([]);
 });
 it("không suy số 0 khi chưa đọc được nguồn",async()=>{
  setup({unavailable:true});fireEvent.click(screen.getByRole("button",{name:"Xóa HTX"}));
  expect(await screen.findByRole("alert")).toBeInTheDocument();
  expect(screen.queryByText(/HTX còn 0 hộ/)).toBeNull();expect(screen.getByRole("button",{name:"Xác nhận xóa HTX"})).toBeDisabled();
 });
 it("xác nhận xóa, khóa gửi đúp và thử lại cùng phiên bản/khóa khi nguồn lỗi",async()=>{
  const {requests}=setup({retry:true});fireEvent.click(screen.getByRole("button",{name:"Xóa HTX"}));
  await screen.findByText("HTX còn 0 hộ, 0 tài khoản.");
  const confirm=screen.getByRole("button",{name:"Xác nhận xóa HTX"});fireEvent.click(confirm);fireEvent.click(confirm);
  const retry=await screen.findByRole("button",{name:"Thử xóa lại"});expect(requests).toHaveLength(1);
  fireEvent.click(retry);await waitFor(()=>expect(requests).toHaveLength(2));
  expect(requests[0]).toEqual(requests[1]);expect(requests[0]!.key).toBeTruthy();expect(requests[0]!.body).toEqual({expected_revision:42});
  await waitFor(()=>expect(navigation.replace).toHaveBeenCalledWith("/htx"));
 });
});
