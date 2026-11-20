import sodium from "libsodium-wrappers";
import type { ApiClient } from "@/api/client";
import { unwrap } from "@/api/client";
import type { components } from "@/api/generated/application";
import { uuidv7 } from "@/api/idempotency";

type Meta = components["schemas"]["SyncEnvelopeMeta"];
export type TaskPhoto = { ref: string; ciphertext: Uint8Array<ArrayBuffer>; meta: Meta };

/** Envelope v1, interoperable with Go nacl/box. Plaintext and DEK never persist. */
export async function sealTaskPhoto(plain: Uint8Array<ArrayBuffer>, publicKey: string, subject: string, capturedAt: string, gps?: Meta["gps"]): Promise<TaskPhoto> {
  let dek: Uint8Array<ArrayBuffer> | undefined;
  try {
    await sodium.ready;
    const pk = sodium.from_base64(publicKey, sodium.base64_variants.ORIGINAL);
    if (pk.length !== 32 || plain.length > 1024 * 1024) throw new Error("Khóa hoặc kích thước ảnh không hợp lệ.");
    dek = crypto.getRandomValues(new Uint8Array(32));
    const nonce = crypto.getRandomValues(new Uint8Array(12));
    const key = await crypto.subtle.importKey("raw", dek, "AES-GCM", false, ["encrypt"]);
    const ciphertext = new Uint8Array(await crypto.subtle.encrypt({ name: "AES-GCM", iv: nonce }, key, plain));
    const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", ciphertext));
    return { ref: uuidv7(), ciphertext, meta: { v: 1, alg: "A256GCM+X25519-SEALEDBOX", subject_ref: subject, nonce: sodium.to_base64(nonce, sodium.base64_variants.ORIGINAL), wrapped_dek: sodium.to_base64(sodium.crypto_box_seal(dek, pk), sodium.base64_variants.ORIGINAL), sha256_ciphertext: sodium.to_hex(digest), content_type: "image/jpeg", captured_at: capturedAt, ...(gps ? { gps } : {}) } };
  } finally { dek?.fill(0); plain.fill(0); }
}

export async function uploadTaskPhoto(api: ApiClient, device: string, photo: TaskPhoto): Promise<string> {
  const form = new FormData();
  form.append("meta", JSON.stringify(photo.meta));
  form.append("file_ref", photo.ref);
  form.append("blob", new Blob([photo.ciphertext], { type: "application/octet-stream" }), "blob.bin");
  const result = await unwrap(api.POST("/v1/files", { params: { header: { "X-Device-Id": device, "Idempotency-Key": photo.ref } }, body: { meta: photo.meta, blob: "", file_ref: photo.ref }, bodySerializer: () => form }));
  return result.file_ref;
}

/** Paint a fresh camera frame with time/GPS, then compress only in memory. */
export async function cameraFrame(video: HTMLVideoElement, capturedAt: string, gps?: Meta["gps"]): Promise<Uint8Array<ArrayBuffer>> {
  if (!video.videoWidth || !video.videoHeight) throw new Error("Camera chưa sẵn sàng.");
  const canvas = document.createElement("canvas");
  try {
    for (const width of [1920, 1440, 1080, 800, 640]) {
      canvas.width = Math.min(width, video.videoWidth); canvas.height = Math.round(video.videoHeight * canvas.width / video.videoWidth);
      const ctx = canvas.getContext("2d"); if (!ctx) throw new Error("Không xử lý được ảnh camera.");
      ctx.drawImage(video, 0, 0, canvas.width, canvas.height);
      const font = Math.max(14, Math.round(canvas.width / 55));
      ctx.fillStyle = "rgba(0,0,0,.8)"; ctx.fillRect(0, canvas.height - font * 3.5, canvas.width, font * 3.5);
      ctx.fillStyle = "white"; ctx.font = `${font}px monospace`;
      ctx.fillText(`AgriGreen ${capturedAt}`, 8, canvas.height - font * 2);
      ctx.fillText(gps ? `GPS: ${gps.lat.toFixed(6)}, ${gps.lon.toFixed(6)}` : "GPS: N/A", 8, canvas.height - font / 2);
      for (const quality of [.85, .7, .55]) {
        const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, "image/jpeg", quality));
        if (blob && blob.size <= 1024 * 1024) return new Uint8Array(await blob.arrayBuffer());
      }
    }
    throw new Error("Không nén được ảnh trong giới hạn 1 MB.");
  } finally { canvas.width = 0; canvas.height = 0; }
}
