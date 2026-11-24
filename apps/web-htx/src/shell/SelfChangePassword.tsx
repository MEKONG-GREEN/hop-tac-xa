"use client";

import { useState, type FormEvent } from "react";
import { unwrap, type ApiClient } from "@/api/client";
import { fieldErrorMap, type ApiProblem } from "@/api/problem";
import { Dialog } from "@/components/Dialog";
import { ProblemAlert } from "@/components/ProblemAlert";
import { toApiProblem } from "@/lib/useAsync";

const MIN_LEN = 6;

/**
 * AH-281 (FN-10/AC-N4, AC-N5): người dùng realm HTX đã đăng nhập tự đổi mật khẩu — mật khẩu hiện tại, mật khẩu mới
 * (tối thiểu 06 ký tự, khác mật khẩu hiện tại), nhập lại. Máy chủ kiểm lại mọi điều kiện; không lưu mật khẩu ở trình duyệt.
 */
export function SelfChangePassword({ api, onClose }: { api: ApiClient; onClose: () => void }) {
  const [form, setForm] = useState({ current_password: "", new_password: "", confirm_password: "" });
  const [local, setLocal] = useState<Record<string, string>>({});
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const errors = { ...(problem ? fieldErrorMap(problem) : {}), ...local };

  const submit = async (e: FormEvent) => {
    e.preventDefault(); if (busy) return;
    const next: Record<string, string> = {};
    if (!form.current_password) next.current_password = "Nhập mật khẩu hiện tại";
    if ([...form.new_password].length < MIN_LEN) next.new_password = "Mật khẩu tối thiểu 06 ký tự";
    else if (form.new_password === form.current_password) next.new_password = "Mật khẩu mới phải khác mật khẩu hiện tại";
    if (form.confirm_password !== form.new_password) next.confirm_password = "Mật khẩu nhập lại không khớp";
    setLocal(next); setProblem(null);
    if (Object.keys(next).length) return;
    setBusy(true);
    try {
      await unwrap(api.PUT("/v1/me/password", { body: form }));
      setForm({ current_password: "", new_password: "", confirm_password: "" });
      setDone(true);
    } catch (err) { setProblem(toApiProblem(err)); } finally { setBusy(false); }
  };

  const fields = [["current_password", "Mật khẩu hiện tại", "current-password"], ["new_password", "Mật khẩu mới", "new-password"], ["confirm_password", "Nhập lại mật khẩu mới", "new-password"]] as const;
  return (
    <Dialog title="Đổi mật khẩu" onClose={onClose}>
      {done ? (
        <div className="stack">
          <p role="status">Đã đổi mật khẩu. Lần đăng nhập sau dùng mật khẩu mới.</p>
          <div className="row"><button type="button" className="btn btn-p" onClick={onClose}>Đóng</button></div>
        </div>
      ) : (
        <form className="stack" onSubmit={(e) => void submit(e)} noValidate aria-label="Đổi mật khẩu">
          {problem && !Object.keys(fieldErrorMap(problem)).length ? <ProblemAlert problem={problem} /> : null}
          {fields.map(([field, label, auto]) => (
            <label className="field" key={field}>{label}
              <input className="input" type="password" value={form[field]} autoComplete={auto} aria-invalid={!!errors[field]}
                onChange={(e) => { setForm((f) => ({ ...f, [field]: e.target.value })); setLocal((l) => ({ ...l, [field]: "" })); }} />
              {errors[field] ? <span className="err">{errors[field]}</span> : null}
            </label>
          ))}
          <div className="row">
            <button className="btn btn-p" type="submit" disabled={busy}>{busy ? "Đang lưu…" : "Đổi mật khẩu"}</button>
            <button className="btn" type="button" disabled={busy} onClick={onClose}>Hủy</button>
          </div>
        </form>
      )}
    </Dialog>
  );
}
