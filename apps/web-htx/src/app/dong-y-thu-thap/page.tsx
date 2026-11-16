"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useState } from "react";
import { useIdempotentAction } from "@/api/useIdempotentAction";
import { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { safeReturnTo } from "@/auth/oidc";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading } from "@/components/ui";
import { BrandMark } from "@/shell/BrandMark";
import { BRAND } from "@/shell/brand";

/**
 * AH-23 / REQ-011 / KH-24 — Đồng ý thu thập dữ liệu: hiện ngay sau đăng nhập, trước khi vào ứng dụng, khi tài khoản HTX
 * chưa đồng ý phiên bản nội dung hiện hành. "Không đồng ý" → đăng xuất (lần đăng nhập sau hỏi lại). Không xác định được
 * trạng thái (lỗi mạng/503) → vẫn chặn, cho thử lại hoặc đăng xuất.
 */
function ConsentInner() {
  const { status, dataConsent, acceptDataConsent, refreshDataConsent, logout } = useAuth();
  const router = useRouter();
  const raw = safeReturnTo(useSearchParams().get("returnTo"));
  const returnTo = raw.startsWith("/dong-y-thu-thap") ? "/" : raw;
  const action = useIdempotentAction();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);

  useEffect(() => {
    if (status === "anonymous") router.replace("/login");
    else if (status === "authenticated" && dataConsent.status === "not_required") router.replace(returnTo);
  }, [status, dataConsent.status, returnTo, router]);

  if (status !== "authenticated" || dataConsent.status === "unknown" || dataConsent.status === "not_required") {
    return (
      <main style={{ display: "grid", placeItems: "center", minHeight: "100vh" }}>
        <Loading label="Đang kiểm tra phiên đăng nhập…" />
      </main>
    );
  }

  const accept = async (version: string) => {
    setBusy(true);
    setProblem(null);
    try {
      // Lời gọi ghi khởi phát đồng bộ trong run(): bấm lại sau lỗi mạng dùng đúng Idempotency-Key cũ.
      await action.run(() => acceptDataConsent(version));
    } catch (e) {
      setProblem(e instanceof ApiProblem ? e : null);
    } finally {
      setBusy(false);
    }
  };

  const retry = async () => {
    setBusy(true);
    try {
      await refreshDataConsent();
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="auth-bg">
      <div className="choose">
        <header className="choose-top">
          <div className="brand-row">
            <BrandMark />
            {BRAND.productName}
          </div>
        </header>
        <main className="choose-body">
          <h1>Đồng ý thu thập dữ liệu</h1>
          {dataConsent.status === "required" ? (
            <div className="choose-msg">
              {dataConsent.message ? (
                <p className="alert alert-warn" role="status" style={{ marginBottom: 10 }}>
                  {dataConsent.message}
                </p>
              ) : null}
              <p style={{ whiteSpace: "pre-wrap" }}>{dataConsent.text}</p>
              <p className="mute small" style={{ marginTop: 10 }}>
                Phiên bản nội dung: {dataConsent.version}
              </p>
              {problem ? <ProblemAlert problem={problem} /> : null}
              {busy ? <Loading label="Đang ghi nhận…" /> : null}
              <div style={{ display: "flex", gap: 10, marginTop: 14 }}>
                <button type="button" className="btn btn-p" disabled={busy} onClick={() => void accept(dataConsent.version)}>
                  Đồng ý
                </button>
                <button type="button" className="btn btn-o" disabled={busy} onClick={() => void logout()}>
                  Không đồng ý
                </button>
              </div>
            </div>
          ) : (
            <div className="choose-msg">
              {dataConsent.problem ? (
                <ProblemAlert problem={dataConsent.problem} />
              ) : (
                <p className="alert alert-bad" role="alert">
                  Chưa xác nhận được trạng thái Đồng ý thu thập dữ liệu. Vui lòng thử lại.
                </p>
              )}
              {busy ? <Loading label="Đang kiểm tra…" /> : null}
              <div style={{ display: "flex", gap: 10, marginTop: 14 }}>
                <button type="button" className="btn btn-p" disabled={busy} onClick={() => void retry()}>
                  Thử lại
                </button>
                <button type="button" className="btn btn-o" disabled={busy} onClick={() => void logout()}>
                  Đăng xuất
                </button>
              </div>
            </div>
          )}
        </main>
      </div>
    </div>
  );
}

export default function DataConsentPage() {
  return (
    <Suspense fallback={null}>
      <ConsentInner />
    </Suspense>
  );
}
