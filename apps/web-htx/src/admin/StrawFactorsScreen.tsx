"use client";

import { useRef, useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import type { AdminGet } from "@/api/types";
import type { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { matrixAllows, rolesWith } from "@/auth/permissions";
import { hasRole } from "@/auth/principal";
import { Dialog } from "@/components/Dialog";
import { ProblemAlert } from "@/components/ProblemAlert";
import { MfaNotice } from "@/components/MfaNotice";
import { Loading, PageHead } from "@/components/ui";
import { FilterBar, SearchBox, type SortState } from "@/components/patterns/list";
import { ListScreen, clientList, type ClientTab } from "@/components/patterns/listScreen";
import { normalizeVi } from "@/cgh/pins";
import { toApiProblem, useAsync } from "@/lib/useAsync";

type Factor=AdminGet<"/v1/cropcycle/straw-factors">["items"][number];
const TABS:ClientTab<Factor>[]=[{id:"all",label:"Tất cả",match:()=>true},{id:"configured",label:"Đã cấu hình",match:(r)=>r.configured},{id:"default",label:"Mặc định 35%",match:(r)=>!r.configured}];

function FactorForm({rows,initial,onClose,onSaved}:{rows:Factor[];initial?:Factor;onClose:()=>void;onSaved:()=>void}){
 const {adminApi}=useAuth();
 const editing=!!initial?.configured;
 const [code,setCode]=useState(initial?.variety_code??"");
 const [percentage,setPercentage]=useState(initial?.percentage??"");
 const [busy,setBusy]=useState(false);
 const [problem,setProblem]=useState<ApiProblem|null>(null);
 const [validation,setValidation]=useState<string|null>(null);
 const key=useRef(newIdempotencyKey());const pending=useRef(false);
 const changed=()=>{key.current=newIdempotencyKey();setValidation(null)};
 const submit=async(e:FormEvent)=>{
  e.preventDefault();if(pending.current)return;
  const pct=percentage.trim().replace(",",".");
  if(!code||!pct){setValidation("Bắt buộc nhập");return}
  if(!/^(?:0|[1-9][0-9]{0,2})(?:\.[0-9]{1,4})?$/.test(pct)||BigInt(pct.split(".")[0]!)>100n||(pct.split(".")[0]==="100"&&/[1-9]/.test(pct.split(".")[1]??""))){setValidation("Hệ số từ 0 đến 100%");return}
  pending.current=true;setBusy(true);setProblem(null);
  try{
   if(editing)await unwrap(adminApi.POST("/v1/cropcycle/straw-factors/{variety_code}/revisions",{params:{path:{variety_code:code},header:{"Idempotency-Key":key.current}},body:{percentage:pct,expected_revision:initial!.revision}}));
   else await unwrap(adminApi.POST("/v1/cropcycle/straw-factors",{params:{header:{"Idempotency-Key":key.current}},body:{variety_code:code,percentage:pct}}));
   onSaved();
  }catch(error){const p=toApiProblem(error);setProblem(p);if(p.status>=400&&p.status<500)key.current=newIdempotencyKey()}
  finally{pending.current=false;setBusy(false)}
 };
 const available=rows.filter((r)=>r.active&&!r.configured);
 return <Dialog title={editing?"Sửa hệ số rơm":"Thêm hệ số rơm"} onClose={()=>{if(!busy)onClose()}}><form noValidate onSubmit={(e)=>void submit(e)}>
  <fieldset disabled={busy} className="stack" onChange={changed}>
   {editing?<p>Giống: {initial!.variety_name} ({code})</p>:<div className="field"><label htmlFor="factor-variety">Giống *</label><select id="factor-variety" className="input" value={code} onChange={(e)=>setCode(e.target.value)}><option value="">Chọn giống lúa</option>{available.map((r)=><option key={r.variety_code} value={r.variety_code}>{r.variety_name}</option>)}</select>{available.length===0?<p>Không còn giống lúa đang hiện chưa có hệ số.</p>:null}</div>}
   <div className="field"><label htmlFor="factor-percentage">Hệ số (%) *</label><input id="factor-percentage" className="input" inputMode="decimal" value={percentage} onChange={(e)=>setPercentage(e.target.value)}/><p className="small mute">Gợi ý 30–40%. Cho phép 0–100%; tối đa 4 chữ số thập phân.</p></div>
   {validation?<p role="alert">{validation}</p>:null}{problem?<ProblemAlert problem={problem}/>:null}
   <button type="submit" className="btn btn-p" disabled={busy}>{busy?"Đang lưu…":"Lưu hệ số"}</button>
  </fieldset>
 </form></Dialog>;
}

function FactorHistory({row,onClose}:{row:Factor;onClose:()=>void}){
 const {adminApi}=useAuth();const [before,setBefore]=useState<number|undefined>();
 const data=useAsync(()=>unwrap(adminApi.GET("/v1/cropcycle/straw-factors/{variety_code}/history",{params:{path:{variety_code:row.variety_code},query:{before_revision:before,limit:20}}})),[adminApi,row.variety_code,before]);
 return <Dialog title={`Lịch sử hệ số — ${row.variety_name}`} onClose={onClose}>
  {data.loading?<Loading/>:data.error?<ProblemAlert problem={data.error} onRetry={data.reload}/>:<>
   <table className="table"><caption>Lịch sử theo giống</caption><thead><tr><th>Hiệu lực từ</th><th>Trước (%)</th><th>Sau (%)</th><th>Người sửa</th></tr></thead><tbody>{data.data?.items.map((e)=><tr key={e.id}><td>{new Date(e.effective_at).toLocaleString("vi-VN")}</td><td>{e.previous_percentage??"Mặc định 35"}</td><td>{e.percentage}</td><td>{e.actor_ref}</td></tr>)}</tbody></table>
   {data.data?.items.length===0?<p>Chưa có lịch sử cấu hình.</p>:null}
   {before?<button className="btn btn-o" onClick={()=>setBefore(undefined)}>Về mới nhất</button>:null}
   {data.data?.next_before_revision?<button className="btn btn-o" onClick={()=>setBefore(data.data!.next_before_revision!)}>Lịch sử cũ hơn</button>:null}
  </>}
 </Dialog>;
}

function Factors(){
 const {adminApi}=useAuth();const data=useAsync(()=>unwrap(adminApi.GET("/v1/cropcycle/straw-factors")),[adminApi]);
 const [tab,setTab]=useState("all"),[q,setQ]=useState("");const [page,setPage]=useState(1);const [sort,setSort]=useState<SortState|null>(null);
 const [edit,setEdit]=useState<{row?:Factor}|null>(null);const [history,setHistory]=useState<Factor|null>(null);const [notice,setNotice]=useState<string|null>(null);
 const rows=!data.loading&&!data.error?data.data?.items??[]:[];const term=normalizeVi(q);
 const view=clientList(rows,{tabs:TABS,tab,filter:(r)=>normalizeVi(r.variety_code+" "+r.variety_name).includes(term),sort,sortValue:(r,k)=>k==="code"?r.variety_code:r.variety_name,page});
 return <><PageHead title="Hệ số rơm" subtitle="Hệ số theo giống cho thống kê CT-5. Vụ đã hoàn thành giữ hệ số có hiệu lực khi hoàn thành." actions={<button className="btn btn-p" onClick={()=>setEdit({})}>Thêm hệ số</button>}/><MfaNotice/>
  {notice?<p role="status">{notice}</p>:null}{data.loading?<Loading/>:null}{data.error?<ProblemAlert problem={data.error} onRetry={data.reload}/>:null}
  {!data.loading&&!data.error?<ListScreen caption="Hệ số theo giống lúa" tabsLabel="Cấu hình hệ số" tabs={view.tabs} tab={tab} onTab={(v)=>{setTab(v);setPage(1)}}
   filterBar={<FilterBar resultCount={view.total} activeCount={q.trim()?1:0} onClear={()=>{setQ("");setPage(1)}}><SearchBox value={q} onChange={(v)=>{setQ(v);setPage(1)}} placeholder="Tìm mã hoặc tên giống"/></FilterBar>}
   code={{key:"code",header:"Mã",sortKey:"code",cell:(r)=>r.variety_code}} main={{key:"name",header:"Giống lúa",sortKey:"name",title:(r)=>r.variety_name,sub:(r)=>r.active?undefined:"Giống đã ẩn",open:(r)=>({onClick:()=>setHistory(r)})}}
   columns={[{key:"percentage",header:"Hệ số (%)",cell:(r)=>r.percentage+"%"},{key:"configured",header:"Nguồn",cell:(r)=>r.configured?"Đã cấu hình":"Mặc định 35%"}]}
   actions={{cell:(r)=><div className="row">{r.configured||r.active?<button className="btn btn-o btn-sm" onClick={()=>setEdit({row:r})}>{r.configured?"Sửa":"Cấu hình"}</button>:null}<button className="btn btn-o btn-sm" onClick={()=>setHistory(r)}>Lịch sử</button></div>}}
   rows={view.rows} rowKey={(r)=>r.variety_code} total={view.total} page={view.page} onPage={setPage} sort={sort} onSort={(s)=>{setSort(s);setPage(1)}} empty="Không có giống phù hợp."/>:null}
  {edit?<FactorForm rows={rows} initial={edit.row} onClose={()=>setEdit(null)} onSaved={()=>{setEdit(null);setNotice("Đã lưu hệ số rơm.");data.reload()}}/>:null}
  {history?<FactorHistory row={history} onClose={()=>setHistory(null)}/>:null}
 </>;
}

export function StrawFactorsScreen(){const {principal}=useAuth();if(!hasRole(principal,...rolesWith("FN-11","R"))||!matrixAllows(principal,"FN-11","R"))return null;return <Factors key={JSON.stringify([principal?.subject,principal?.tenantId,principal?.roles])}/>}
