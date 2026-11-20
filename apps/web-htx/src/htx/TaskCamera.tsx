"use client";

import { useEffect, useRef, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { unwrap } from "@/api/client";
import { uuidv7 } from "@/api/idempotency";
import type { TaskPhoto } from "./taskPhoto";
import { cameraFrame, sealTaskPhoto } from "./taskPhoto";
import { cachedTaskRead } from "./taskJournalQueue";

export function TaskCamera({ household, count, disabled, onPhoto }: { household: string; count: number; disabled: boolean; onPhoto: (photo: TaskPhoto, device: string) => void }) {
  const { api, principal } = useAuth();
  const video = useRef<HTMLVideoElement>(null);
  const stream = useRef<MediaStream | null>(null);
  const live = useRef(true);
  const device = useRef(uuidv7());
  const [active, setActive] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const stop = () => { stream.current?.getTracks().forEach((track) => track.stop()); stream.current = null; };
  useEffect(() => { live.current = true; return () => { live.current = false; stop(); }; }, []);
  const start = async () => {
    setBusy(true); setError("");
    try {
      if (!navigator.mediaDevices?.getUserMedia) throw new Error("Trình duyệt cần HTTPS và camera để chụp ảnh trực tiếp.");
      const media = await navigator.mediaDevices.getUserMedia({ audio: false, video: { facingMode: { ideal: "environment" }, width: { ideal: 1920 } } });
      if (!live.current) { media.getTracks().forEach((track) => track.stop()); return; }
      stop(); stream.current = media; setActive(true);
      if (video.current) { video.current.srcObject = media; await video.current.play(); }
    } catch (e) { stop(); setActive(false); setError(e instanceof Error ? e.message : "Không mở được camera."); }
    finally { if (live.current) setBusy(false); }
  };
  const capture = async () => {
    if (!video.current || count >= 5 || disabled) return;
    setBusy(true); setError("");
    let plain: Uint8Array<ArrayBuffer> | undefined;
    try {
      if (!principal) throw new Error("Cần đăng nhập để chụp ảnh nhật ký.");
      const key = await cachedTaskRead(principal, `photo-key:${household}`, () => unwrap(api.GET("/v1/households/{ref}/photo-key", { params: { path: { ref: household } } })));
      const gps = await new Promise<{ lat: number; lon: number; accuracy_m?: number } | undefined>((resolve) => {
        if (!navigator.geolocation) { resolve(undefined); return; }
        try { navigator.geolocation.getCurrentPosition((p) => resolve({ lat: p.coords.latitude, lon: p.coords.longitude, accuracy_m: p.coords.accuracy }), () => resolve(undefined), { timeout: 2000, maximumAge: 0 }); } catch { resolve(undefined); }
      });
      const at = new Date().toISOString();
      plain = await cameraFrame(video.current, at, gps);
      const photo = await sealTaskPhoto(plain, key.public_key, household, at, gps);
      if (live.current) onPhoto(photo, device.current);
      stop(); setActive(false);
    } catch (e) { setError(e instanceof Error ? e.message : "Chưa chụp được ảnh."); }
    finally { plain?.fill(0); if (live.current) setBusy(false); }
  };
  return <section aria-label="Ảnh minh chứng">
    <p>{count}/5 ảnh · Chụp trực tiếp, nén tối đa 1 MB và đóng dấu thời gian/GPS.</p>
    <video ref={video} hidden={!active} playsInline muted aria-label="Camera trực tiếp" style={{ maxWidth: "100%" }} />
    {active ? <><button type="button" disabled={busy || disabled} onClick={() => void capture()}>Chụp ảnh</button><button type="button" disabled={busy} onClick={() => { stop(); setActive(false); }}>Đóng camera</button></> : <button type="button" disabled={disabled || busy || count >= 5} onClick={() => void start()}>Mở camera</button>}
    {error ? <p role="alert">{error}</p> : null}
  </section>;
}
