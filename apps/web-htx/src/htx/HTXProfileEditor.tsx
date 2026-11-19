"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { listChildUnits } from "@/api/adapters/units";
import { newIdempotencyKey } from "@/api/idempotency";
import type { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { MfaNotice } from "@/components/MfaNotice";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading, PageHead } from "@/components/ui";
import { toApiProblem, useAsync } from "@/lib/useAsync";
import { canEditHTXProfile, createHTXProfile, getHTXProfile, updateHTXProfile, type HTXProfile, type HTXProfileInput } from "./profiles";

function ProfileForm({ profile, onReload }: { profile?: HTXProfile; onReload?: () => void }) {
  const { api, adminApi, principal, refreshPrincipal } = useAuth();
  const router = useRouter();
  const [name, setName] = useState(profile?.name ?? "");
  const [province, setProvince] = useState(profile?.province_code ?? "");
  const [commune, setCommune] = useState(profile?.admin_unit_code ?? "");
  const [address, setAddress] = useState(profile?.address ?? "");
  const [representative, setRepresentative] = useState(profile?.representative_name ?? "");
  const [phone, setPhone] = useState(profile?.phone ?? "");
  const [error, setError] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  const [saved, setSaved] = useState(false);
  const sending = useRef(false);
  const mounted = useRef(true);
  // PII và bản gửi thử lại chỉ ở bộ nhớ của form, không local/sessionStorage.
  const pending = useRef<{ body: HTXProfileInput; key: string } | null>(null);
  const identity = `${principal?.subject}:${principal?.tenantId}:${principal?.roles.join("|")}`;
  const provinces = useAsync(() => listChildUnits(api), [api, identity]);
  const communes = useAsync(() => province ? listChildUnits(api, province) : Promise.resolve([]), [api, province, identity]);
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (sending.current || saved) return;
    sending.current = true; setBusy(true); setError(null);
    pending.current ??= { body: { name: name.trim(), commune_code: commune, head_office: address.trim(), representative_name: representative.trim(), phone: phone.trim() }, key: newIdempotencyKey() };
    try {
      const request = pending.current;
      const result = profile ? await updateHTXProfile(api, profile, request.body, request.key) : await createHTXProfile(adminApi, request.body, request.key);
      if (!mounted.current) return;
      setSaved(true); setUncertain(false); pending.current = null;
      if (profile?.ref === principal?.orgRef) void refreshPrincipal?.().catch(() => undefined);
      router.replace(`/htx/${encodeURIComponent(result.ref)}`);
    } catch (cause) {
      if (!mounted.current) return;
      const problem = toApiProblem(cause);
      setError(problem);
      const unknownOutcome = problem.status === 0 || problem.status >= 500 || problem.code === "idempotency_in_progress";
      setUncertain(unknownOutcome);
      if (!unknownOutcome) pending.current = null;
    } finally {
      sending.current = false;
      if (mounted.current) setBusy(false);
    }
  }

  const frozen = busy || uncertain || saved;
  return <>
    <PageHead title={profile ? "Sửa hồ sơ HTX" : "Thêm HTX"} subtitle={profile ? profile.code : "Mã HTX do hệ thống tự sinh"} />
    {hasRole(principal, ROLES.platformAdmin) ? <MfaNotice what="Quản lý hồ sơ HTX" /> : null}
    <form className="card sec stack" onSubmit={(event) => void submit(event)} aria-label={profile ? "Sửa hồ sơ HTX" : "Thêm HTX"}>
      {error ? <ProblemAlert problem={error} /> : null}
      {uncertain ? <p role="status">Lần lưu trước chưa hoàn tất. Bấm “Thử lưu lại” để tiếp tục.</p> : null}
      {error?.code === "party.cooperative_profile_changed" && onReload ? <button type="button" className="btn btn-o" onClick={onReload}>Tải lại hồ sơ</button> : null}
      {provinces.error ? <ProblemAlert problem={provinces.error} onRetry={provinces.reload} /> : null}
      {communes.error ? <ProblemAlert problem={communes.error} onRetry={communes.reload} /> : null}
      <fieldset disabled={frozen} className="stack">
        {profile ? <label className="field">Mã HTX<input className="input" value={profile.code} readOnly /></label> : null}
        <label className="field">Tên HTX<input className="input" required maxLength={200} value={name} onChange={(event) => setName(event.target.value)} /></label>
        <div className="row">
          <label className="field">Tỉnh / thành phố<select className="select" required value={province} onChange={(event) => { setProvince(event.target.value); setCommune(""); }}>
            <option value="">Chọn tỉnh / thành phố</option>
            {profile?.province_code === province && province && !provinces.data?.some((unit) => unit.code === province) ? <option value={province}>{profile.province_name ?? province} (đang lưu)</option> : null}
            {provinces.data?.map((unit) => <option key={unit.code} value={unit.code}>{unit.name}</option>)}
          </select></label>
          <label className="field">Xã / phường / đặc khu<select className="select" required value={commune} disabled={!province || frozen || communes.loading} onChange={(event) => setCommune(event.target.value)}>
            <option value="">Chọn xã / phường / đặc khu</option>
            {profile?.admin_unit_code === commune && commune && !communes.data?.some((unit) => unit.code === commune) ? <option value={commune}>{profile.commune_name ?? commune} (đang lưu)</option> : null}
            {communes.data?.map((unit) => <option key={unit.code} value={unit.code}>{unit.name}</option>)}
          </select></label>
        </div>
        <label className="field">Địa chỉ trụ sở<textarea className="input" required rows={3} maxLength={500} value={address} onChange={(event) => setAddress(event.target.value)} /></label>
        <label className="field">Người đại diện (không bắt buộc)<input className="input" maxLength={200} value={representative} onChange={(event) => setRepresentative(event.target.value)} /></label>
          <label className="field">Số điện thoại<input className="input" type="tel" required pattern="0[0-9]{9}" maxLength={10} inputMode="tel" value={phone} onChange={(event) => setPhone(event.target.value)} aria-describedby="htx-phone-help" /></label>
          <span id="htx-phone-help" className="mute small">SĐT gồm 10 chữ số, bắt đầu bằng 0</span>
      </fieldset>
      <div className="row">
        <button type="submit" className="btn btn-p" disabled={busy || saved || provinces.loading || communes.loading || Boolean(provinces.error || communes.error)}>{busy ? "Đang lưu…" : saved ? "Đã lưu" : uncertain ? "Thử lưu lại" : "Lưu hồ sơ"}</button>
        {!frozen ? <Link className="btn btn-o" href={profile ? `/htx/${encodeURIComponent(profile.ref)}` : "/htx"}>Quay lại</Link> : null}
      </div>
    </form>
  </>;
}

export function HTXProfileEditor({ cooperativeRef }: { cooperativeRef?: string }) {
  const { api, principal } = useAuth();
  const allowed = canEditHTXProfile(principal, cooperativeRef);
  const identity = `${principal?.subject}:${principal?.tenantId}:${principal?.orgRef}:${principal?.roles.join("|")}`;
  const profile = useAsync(() => cooperativeRef && allowed ? getHTXProfile(api, cooperativeRef) : Promise.resolve(null), [api, cooperativeRef, allowed, identity]);
  if (!principal) return <Loading />;
  if (!allowed) return <div className="alert alert-bad" role="alert">Bạn không có quyền {cooperativeRef ? "sửa hồ sơ HTX này" : "thêm HTX"}. <Link href="/htx">Về danh sách HTX</Link></div>;
  if (cooperativeRef) {
    if (profile.error) return <ProblemAlert problem={profile.error} onRetry={profile.reload} />;
    if (!profile.data || profile.loading) return <Loading />;
    if (!profile.data.can_edit || profile.data.contact_masked || !profile.data.active) return <div className="alert alert-bad" role="alert">Hồ sơ HTX này chỉ được xem.</div>;
    return <ProfileForm key={`${identity}:${profile.data.ref}:${profile.data.revision}`} profile={profile.data} onReload={profile.reload} />;
  }
  return <ProfileForm key={identity} />;
}
