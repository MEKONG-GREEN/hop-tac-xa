import { webcrypto } from "node:crypto";
import sodium from "libsodium-wrappers";
import { expect, it, vi } from "vitest";
import { sealTaskPhoto } from "./taskPhoto";

it("AH-99 envelope uses Go-compatible sealed box and AES-GCM; wipes plaintext", async () => {
  vi.stubGlobal("crypto", webcrypto); await sodium.ready;
  const pair = sodium.crypto_box_keypair(); const plain = new TextEncoder().encode("camera-only test frame"); const original = plain.slice();
  const photo = await sealTaskPhoto(plain, sodium.to_base64(pair.publicKey, sodium.base64_variants.ORIGINAL), "household", "2026-11-20T10:00:00+07:00");
  expect(plain.every((b) => b === 0)).toBe(true);
  const dek = sodium.crypto_box_seal_open(sodium.from_base64(photo.meta.wrapped_dek, sodium.base64_variants.ORIGINAL), pair.publicKey, pair.privateKey);
  const key = await crypto.subtle.importKey("raw", new Uint8Array(dek), "AES-GCM", false, ["decrypt"]);
  const opened = await crypto.subtle.decrypt({ name: "AES-GCM", iv: new Uint8Array(sodium.from_base64(photo.meta.nonce, sodium.base64_variants.ORIGINAL)) }, key, photo.ciphertext);
  expect(Array.from(new Uint8Array(opened))).toEqual(Array.from(original)); expect(photo.ciphertext.length).toBe(original.length + 16);
  expect(photo.meta.sha256_ciphertext).toBe(sodium.to_hex(new Uint8Array(await crypto.subtle.digest("SHA-256", photo.ciphertext))));
});
