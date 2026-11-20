import { expect, test } from "@playwright/test";

test.use({
  channel: process.env.PLAYWRIGHT_CHANNEL,
  permissions: ["camera", "microphone"],
  launchOptions: { args: ["--use-fake-device-for-media-stream", "--use-fake-ui-for-media-stream"] },
});

test("AH-99 browser permits same-origin camera and keeps microphone blocked", async ({ page }) => {
  await page.goto("/login");
  const result = await page.evaluate(async () => {
    const stream = await navigator.mediaDevices.getUserMedia({ video: true, audio: false });
    const tracks = stream.getVideoTracks();
    const label = tracks[0]?.label;
    const width = tracks[0]?.getSettings().width;
    stream.getTracks().forEach((t) => t.stop());
    const stopped = tracks.every((t) => t.readyState === "ended");
    let microphoneBlocked = false;
    try {
      const audio = await navigator.mediaDevices.getUserMedia({ audio: true, video: false });
      audio.getTracks().forEach((t) => t.stop());
    } catch (e) { microphoneBlocked = e instanceof DOMException && e.name === "NotAllowedError"; }
    return { label, width, stopped, microphoneBlocked };
  });
  expect(result.label).toContain("fake");
  expect(result.width).toBeGreaterThan(0);
  expect(result.stopped).toBe(true);
  expect(result.microphoneBlocked).toBe(true);
});
