export type RelayFingerprint = {
  os: string;
  cpu: Record<string, number | null>;
  screen: Record<string, number | null>;
  fonts: string[];
  canvas: string;
  webgl: Record<string, unknown>;
  audio: string;
  browser: Record<string, unknown>;
};

async function hashText(value: string) {
  if (!value || !crypto?.subtle || !TextEncoder) return "";
  try {
    const buffer = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(String(value)));
    return Array.from(new Uint8Array(buffer)).map((item) => item.toString(16).padStart(2, "0")).join("").slice(0, 24);
  } catch { return ""; }
}

function detectOs() {
  const nav = navigator as Navigator & { userAgentData?: { platform?: string } };
  const uaPlatform = nav.userAgentData?.platform || navigator.platform || navigator.userAgent || "";
  const ua = String(uaPlatform).toLowerCase() + " " + String(navigator.userAgent || "").toLowerCase();
  if (ua.includes("android")) return "Android";
  if (ua.includes("iphone") || ua.includes("ipad") || ua.includes("ipod")) return "iOS";
  if (ua.includes("win")) return "Windows";
  if (ua.includes("mac")) return "macOS";
  if (ua.includes("linux")) return "Linux";
  return "未知";
}

function collectFonts() {
  const baseFonts = ["monospace", "sans-serif", "serif"];
  const candidates = ["Arial", "Helvetica", "Times New Roman", "Courier New", "Verdana", "Georgia", "Trebuchet MS", "Comic Sans MS", "Impact", "Segoe UI", "PingFang SC", "Microsoft YaHei", "Noto Sans", "Roboto"];
  const span = document.createElement("span");
  span.style.cssText = "position:absolute;left:-9999px;font-size:72px;visibility:hidden";
  span.textContent = "mmmmmmmmmmlli";
  const sizes = new Map<string, string>();
  for (const base of baseFonts) { span.style.fontFamily = base; document.body.appendChild(span); sizes.set(base, `${span.offsetWidth}:${span.offsetHeight}`); span.remove(); }
  return candidates.filter((font) => baseFonts.some((base) => { span.style.fontFamily = `'${font}',${base}`; document.body.appendChild(span); const different = `${span.offsetWidth}:${span.offsetHeight}` !== sizes.get(base); span.remove(); return different; }));
}

async function collectCanvasHash() {
  try {
    const canvas = document.createElement("canvas"); const context = canvas.getContext("2d"); if (!context) return "";
    canvas.width = 280; canvas.height = 80; context.fillStyle = "#f60"; context.fillRect(10, 10, 100, 40); context.fillStyle = "#069"; context.font = "16px Arial"; context.fillText("tg-bot-fingerprint", 14, 38); context.strokeStyle = "rgba(120, 30, 200, 0.8)"; context.beginPath(); context.arc(180, 36, 20, 0, Math.PI * 2); context.stroke();
    return hashText(canvas.toDataURL());
  } catch { return ""; }
}

async function collectAudioHash() {
  try {
    const AudioClass = (window as unknown as { OfflineAudioContext?: typeof OfflineAudioContext; webkitOfflineAudioContext?: typeof OfflineAudioContext }).OfflineAudioContext || (window as unknown as { webkitOfflineAudioContext?: typeof OfflineAudioContext }).webkitOfflineAudioContext;
    if (!AudioClass) return "";
    const context = new AudioClass(1, 44100, 44100); const oscillator = context.createOscillator(); const compressor = context.createDynamicsCompressor(); oscillator.type = "triangle"; oscillator.frequency.value = 1000; oscillator.connect(compressor); compressor.connect(context.destination); oscillator.start(0); const rendered = await context.startRendering(); const channel = rendered.getChannelData(0).slice(0, 128); oscillator.disconnect(); compressor.disconnect(); return hashText(Array.from(channel).join(","));
  } catch { return ""; }
}

async function collectWebGl() {
  try {
    const canvas = document.createElement("canvas"); const context = canvas.getContext("webgl"); if (!context) return {};
    const debugInfo = context.getExtension("WEBGL_debug_renderer_info");
    const payload = { vendor: debugInfo ? context.getParameter(debugInfo.UNMASKED_VENDOR_WEBGL) : context.getParameter(context.VENDOR), renderer: debugInfo ? context.getParameter(debugInfo.UNMASKED_RENDERER_WEBGL) : context.getParameter(context.RENDERER), version: context.getParameter(context.VERSION), shadingLanguageVersion: context.getParameter(context.SHADING_LANGUAGE_VERSION) };
    return { ...payload, hash: await hashText(JSON.stringify(payload)) };
  } catch { return {}; }
}

export async function collectRelayFingerprint(): Promise<RelayFingerprint> {
  const nav = navigator as Navigator & { deviceMemory?: number };
  const [canvas, webgl, audio] = await Promise.all([collectCanvasHash(), collectWebGl(), collectAudioHash()]);
  return {
    os: detectOs(),
    cpu: { hardwareConcurrency: navigator.hardwareConcurrency || null, deviceMemory: nav.deviceMemory || null, maxTouchPoints: navigator.maxTouchPoints || 0 },
    screen: { width: screen.width || null, height: screen.height || null, availWidth: screen.availWidth || null, availHeight: screen.availHeight || null, colorDepth: screen.colorDepth || null, pixelDepth: screen.pixelDepth || null, pixelRatio: devicePixelRatio || null },
    fonts: collectFonts(), canvas, webgl, audio,
    browser: { language: navigator.language || "", languages: Array.isArray(navigator.languages) ? navigator.languages : [], platform: navigator.platform || "", userAgent: navigator.userAgent || "" },
  };
}

export function collectWebRtcIps(timeout = 1800): Promise<string[]> {
  return new Promise((resolve) => {
    const found = new Set<string>();
    if (typeof RTCPeerConnection === "undefined") { resolve([]); return; }
    const peer = new RTCPeerConnection({ iceServers: [{ urls: "stun:stun.miwifi.com:3478" }] });
    const store = (value?: string | null) => { if (value && (/^(?:\d{1,3}\.){3}\d{1,3}$/.test(value) || value.includes(":"))) found.add(value); };
    peer.createDataChannel("ip");
    peer.onicecandidate = (event) => { store(event.candidate?.address); const parts = event.candidate?.candidate?.trim().split(/\s+/); if (parts && parts.length >= 5) store(parts[4]); };
    peer.createOffer().then((offer) => peer.setLocalDescription(offer)).catch(() => undefined);
    window.setTimeout(() => { peer.close(); resolve(Array.from(found)); }, timeout);
  });
}
