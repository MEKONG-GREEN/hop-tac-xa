// Demo-only FN-11 source; percentages remain decimal strings. Production uses
// the append-only owner ledger and its time-effective port.
import type { components } from "../generated/admin";
import { HTX_ROLE_MATRIX } from "../generated/htx-roles";
import { uuidv7 } from "../idempotency";
import { CATALOGS } from "./data";
import { json, nowIso, problem, validation, type Route } from "./http";
import type { MockPersona } from "./personas";

type Event=components["schemas"]["StrawFactorEvent"];
const events=new Map<string,Event[]>();
export function strawFactorReferenced(code:string){return [...events.values()].some((rows)=>rows.some((e)=>e.variety_code===code))}
const can=(p:MockPersona,a:string)=>{const role=HTX_ROLE_MATRIX.roles.find((r)=>p.me.roles.includes(r.key))?.key;const fn=HTX_ROLE_MATRIX.functions.find((f)=>f.key==="FN-11");return !!role&&!!fn?.grants[role].includes(a)};
const rows=(p:MockPersona)=>events.get(p.me.tenant_id)??[];
const invalid=(field:string,reason:string)=>validation([{field,reason}]);
export const strawFactorRoutes:Route[]=[
 {method:"GET",pattern:/^\/v1\/cropcycle\/straw-factors$/,handler:({persona})=>{
  if(!can(persona,"R"))return problem(403,"forbidden","Không có quyền xem hệ số rơm");
  const rice=new Set(CATALOGS.crop.filter((c)=>c.attrs.is_rice==="true").map((c)=>c.code));
  const items=CATALOGS.rice_variety.filter((v)=>rice.has(v.attrs.crop_code??"")).map((v)=>{const e=rows(persona).find((e)=>e.variety_code===v.code);return {variety_code:v.code,variety_name:v.name,active:v.active,configured:!!e,percentage:e?.percentage??"35",revision:e?.revision??0,effective_at:e?.effective_at??null}});
  return json({items});
 }},
 {method:"GET",pattern:/^\/v1\/cropcycle\/straw-factors\/([^/]+)\/history$/,handler:({persona,params,url})=>{
  if(!can(persona,"R"))return problem(403,"forbidden","Không có quyền xem lịch sử");
  const limit=Math.min(Math.max(Number(url.searchParams.get("limit")??20)||20,1),99),before=Number(url.searchParams.get("before_revision")??0);
  const all=rows(persona).filter((e)=>e.variety_code===decodeURIComponent(params[0]!)&&(!before||e.revision<before));
  return json({items:all.slice(0,limit),next_before_revision:all.length>limit?all[limit-1]!.revision:null});
 }},
 {method:"POST",pattern:/^\/v1\/cropcycle\/straw-factors(?:\/([^/]+)\/revisions)?$/,handler:({persona,params,body})=>{
  const update=!!params[0];if(!can(persona,update?"U":"C"))return problem(403,"forbidden","Không có quyền ghi hệ số");
  const b=(body??{})as {variety_code?:string;percentage?:string;expected_revision?:number};const code=update?decodeURIComponent(params[0]!):b.variety_code;
  if(!code)return invalid("variety_code","Bắt buộc nhập");if(!b.percentage)return invalid("percentage","Bắt buộc nhập");if(typeof b.percentage!=="string")return invalid("percentage","Hệ số từ 0 đến 100%");
  const pct=b.percentage.trim();if(!/^(?:0|[1-9][0-9]{0,2})(?:\.[0-9]{1,4})?$/.test(pct)||BigInt(pct.split(".")[0]!)>100n||(pct.split(".")[0]==="100"&&/[1-9]/.test(pct.split(".")[1]??"")))return invalid("percentage","Hệ số từ 0 đến 100%");
  const v=CATALOGS.rice_variety.find((v)=>v.code===code),crop=CATALOGS.crop.find((c)=>c.code===v?.attrs.crop_code);
  if(!v||crop?.attrs.is_rice!=="true"||(!update&&!v.active))return invalid("variety_code","Chọn giống lúa đang sử dụng");
  const current=rows(persona).find((e)=>e.variety_code===code);
  if(!update&&current)return problem(409,"cropcycle.straw_factor_taken","Giống này đã có hệ số");
  if(update&&!current)return problem(404,"cropcycle.straw_factor_not_found","Giống chưa có hệ số được cấu hình");
  if(update&&b.expected_revision!==current!.revision)return problem(409,"cropcycle.straw_factor_concurrent","Hệ số đã thay đổi; tải lại trước khi sửa");
  const at=nowIso();const e:Event={id:uuidv7(),variety_code:code,percentage:pct,previous_percentage:current?.percentage??null,revision:(current?.revision??0)+1,effective_at:at,recorded_at:at,actor_ref:persona.me.actor_ref};
  events.set(persona.me.tenant_id,[e,...rows(persona)]);return json(e,update?200:201);
 }}
];
