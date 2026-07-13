import { useEffect, useMemo, useRef, useState } from "react";
import { collectRelayFingerprint, collectWebRtcIps } from "./fingerprint";

type View = "home" | "handoff" | "chat" | "npc";
type MobilePane = "groups" | "messages";

type ChatMessage = {
  id: string;
  sender: string;
  avatar: string;
  avatarUrl?: string;
  tone: "mint" | "pink" | "yellow" | "lilac" | "bot" | "self";
  text: string;
  time: string;
  reportable?: boolean;
  correctReport?: boolean;
  isBot?: boolean;
};

type ChatGroup = {
  id: string;
  name: string;
  tag: string;
  color: "pink" | "mint" | "yellow" | "lilac";
  preview: string;
  unread?: number;
  messages: ChatMessage[];
};

type NPC = {
  id: number;
  name: string;
  tg_username: string;
  description: string;
  avatar_url: string;
};
type Achievement = { code: string; name: string; description: string; unlocked: boolean };
type LevelScriptMessage = { send_id: number; text: string; reportable?: boolean };
type LevelScript = {
  group_id: string;
  level_no: number;
  npc_id: number[];
  npc_photo: Record<number, string>;
  messages: LevelScriptMessage[];
};
type LevelSubmissionMeta = { group_id: string; npc_ids: number[]; editor_prompt: string };
type AdminOverview = {
  counts: Record<string, number>;
  npcs: NPC[];
  npc_applications: Array<{ id: string; name: string; tg_username: string; description: string; status: string; match_label: string; match_score: number; created_at: string }>;
  level_submissions: Array<{ id: string; name: string; description: string; payload: string; status: string; match_label: string; match_score: number; created_at: string }>;
  fingerprints: Array<{ tg_user_id: string; fingerprint_id: string; fingerprint: Record<string, unknown>; last_seen_at: string }>;
  fingerprint_labels: Array<{ id: string; label_name: string; fingerprint_id: string; fingerprint: Record<string, unknown>; rules: string[]; updated_at: string }>;
};
type PreparedFingerprint = {
  fingerprint: Awaited<ReturnType<typeof collectRelayFingerprint>>;
  webrtcIps: string[];
};
type TelegramWebApp = {
  initData?: string;
  initDataUnsafe?: { user?: { id?: number; first_name?: string; last_name?: string; username?: string; photo_url?: string; language_code?: string }; start_param?: string; query_id?: string };
  colorScheme?: string;
  themeParams?: Record<string, string>;
  platform?: string;
  version?: string;
  ready?: () => void;
  expand?: () => void;
  disableVerticalSwipes?: () => void;
  setHeaderColor?: (color: string) => void;
  setBackgroundColor?: (color: string) => void;
};

const API_URL = import.meta.env.VITE_API_URL ?? "http://localhost:8080";
const TURNSTILE_SITE_KEY = import.meta.env.VITE_TURNSTILE_SITE_KEY ?? "";
const AUTH_VERSION = "cf-fingerprint-relay-v2";
const fallbackNPCs: NPC[] = [
  { id: 0, name: "顶尖哥", tg_username: "@anlianxiaoliu", description: "他很顶尖。", avatar_url: "" },
  { id: 9478, name: "小孩哥", tg_username: "@xiaohai", description: "到处索要代理节点，要不到就开始嘴硬。", avatar_url: "" },
  { id: 1, name: "群规机器人", tg_username: "@thisisabot", description: "负责审核举报、封禁违规账号并发放成就。", avatar_url: "" },
];

const npcProfiles: Record<number, { name: string; avatar: string; tone: ChatMessage["tone"] }> = {
  0: { name: "顶尖哥", avatar: "顶", tone: "mint" },
  1: { name: "群规机器人", avatar: "安", tone: "bot" },
};

function randomNPCProfile() {
  const adjectives = ["黑调", "悲伤", "暴躁", "迷路", "冷门", "发呆", "阴暗", "嘴硬", "离谱", "困惑", "急眼", "沉默"];
  const nouns = ["迪克", "土豆", "海豹", "番茄", "螺丝", "电池", "薯条", "键盘", "乌云", "汽水", "面包", "路灯"];
  const adjective = adjectives[Math.floor(Math.random() * adjectives.length)];
  const noun = nouns[Math.floor(Math.random() * nouns.length)];
  const number = String(Math.floor(Math.random() * 900) + 100);
  return { name: `${adjective}的${noun}${number}`, avatar: noun.slice(0, 1), tone: "pink" as ChatMessage["tone"] };
}

const fallbackLevelNos: Record<string, number> = { "night-watch": 10001, station: 30001 };
const levelGroups = [
  { id: "night-watch", name: "抓小孩", description: "心智不成熟，满口胡话，想要凑近乎白嫖代理节点。" },
  { id: "station", name: "胡说哥传奇", description: "满口胡话，假装高手，实际上不懂技术。" },
];
const fingerprintFeatureRows = [
  { key: "ip", label: "IP" },
  { key: "asn", label: "ASN" },
  { key: "isp", label: "ISP" },
  { key: "webrtc_ip", label: "webrtc ip" },
  { key: "webrtc_asn", label: "webrtc asn" },
  { key: "webrtc_isp", label: "webrtc isp" },
  { key: "canvas", label: "canvas指纹" },
  { key: "webgl", label: "webgl指纹" },
  { key: "audio", label: "audio指纹" },
  { key: "system", label: "系统" },
  { key: "cpu", label: "cpu" },
  { key: "screen", label: "screen" },
  { key: "fonts", label: "fonts" },
];
function getTelegramWebApp(): TelegramWebApp | undefined {
  return (window as Window & { Telegram?: { WebApp?: TelegramWebApp } }).Telegram?.WebApp;
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {};
}

function fullText(value: unknown, fallback = "无") {
  const text = typeof value === "string" ? value : JSON.stringify(value ?? "");
  return !text || text === "\"\"" ? fallback : text;
}

function uniqueStrings(values: string[]) {
  return Array.from(new Set(values.map((item) => item.trim()).filter(Boolean)));
}

function fingerprintFeatureValues(fingerprint: Record<string, unknown>, key: string) {
  const publicIp = asRecord(fingerprint.publicIpInfo);
  const details = asRecord(fingerprint.details);
  const webrtc = Array.isArray(fingerprint.webrtcIpInfos) ? fingerprint.webrtcIpInfos.map(asRecord) : [];
  switch (key) {
    case "ip": return uniqueStrings([fullText(publicIp.ip, "")]);
    case "asn": return uniqueStrings([fullText(publicIp.asn, "")]);
    case "isp": return uniqueStrings([fullText(publicIp.organization, "")]);
    case "webrtc_ip": return uniqueStrings(webrtc.map((item) => fullText(item.ip, "")));
    case "webrtc_asn": return uniqueStrings(webrtc.map((item) => fullText(item.asn, "")));
    case "webrtc_isp": return uniqueStrings(webrtc.map((item) => fullText(item.organization, "")));
    case "canvas": return uniqueStrings([fullText(details.canvas, "")]);
    case "webgl": {
      const webgl = asRecord(details.webgl);
      return uniqueStrings([fullText(webgl.hash || details.webgl, "")]);
    }
    case "audio": return uniqueStrings([fullText(details.audio, "")]);
    case "system": return uniqueStrings([fullText(details.os, "")]);
    case "cpu": return uniqueStrings([fullText(details.cpu, "")]);
    case "screen": return uniqueStrings([fullText(details.screen, "")]);
    case "fonts": return uniqueStrings(Array.isArray(details.fonts) ? details.fonts.map((item) => fullText(item, "")) : [fullText(details.fonts, "")]);
    default: return [];
  }
}

function reviewMatchText(item: { match_label?: string; match_score?: number }) {
  if (!item.match_label || !item.match_score) return "";
  return `${item.match_label}（${Math.round(item.match_score * 100)}%）`;
}

function statusText(status: string) {
  const map: Record<string, string> = { pending: "待审核", approved: "已通过", rejected: "已拒绝" };
  return map[status] ?? status;
}

function adminCountText(key: string) {
  const map: Record<string, string> = {
    npcs: "角色",
    npc_applications: "角色申请",
    level_submissions: "关卡申请",
    players: "玩家",
    achievements: "成就",
    fingerprint_labels: "标签",
    fingerprints: "指纹",
  };
  return map[key] ?? key;
}

function createLevelProfileMap(level: LevelScript) {
  const profiles: Record<number, { name: string; avatar: string; tone: ChatMessage["tone"] }> = {};
  level.npc_id.forEach((id) => {
    profiles[id] = npcProfiles[id] ?? randomNPCProfile();
  });
  level.messages.forEach((message) => {
    profiles[message.send_id] = profiles[message.send_id] ?? npcProfiles[message.send_id] ?? randomNPCProfile();
  });
  return profiles;
}

function levelMessageToChat(level: LevelScript, item: LevelScriptMessage, index: number, profiles: Record<number, { name: string; avatar: string; tone: ChatMessage["tone"] }>): ChatMessage {
  const profile = profiles[item.send_id] ?? npcProfiles[item.send_id] ?? randomNPCProfile();
  return {
    id: `${level.group_id}-${level.level_no}-${index}`,
    sender: profile.name,
    avatar: profile.avatar,
    avatarUrl: level.npc_photo[item.send_id] || "",
    tone: profile.tone,
    isBot: profile.tone === "bot",
    text: item.text,
    time: "刚刚",
    reportable: item.reportable,
    correctReport: level.group_id === "night-watch" && !!item.reportable,
  };
}

const groups: ChatGroup[] = [
  {
    id: "night-watch",
    name: "抓小孩",
    tag: `${fallbackLevelNos["night-watch"]} 人`,
    color: "pink",
    preview: "群友：又来问节点了…",
    unread: 2,
    messages: [],
  },
  {
    id: "station",
    name: "胡说哥传奇",
    tag: `${fallbackLevelNos.station} 人`,
    color: "mint",
    preview: "顶尖哥：大家好…",
    messages: [],
  },
  {
    id: "paper-club",
    name: "成就",
    tag: "27 人",
    color: "lilac",
    preview: "米粒：今晚放映《云朵信使》",
    messages: [
      {
        id: "club-01",
        sender: "米粒",
        avatar: "米",
        tone: "lilac",
        text: "今晚放映《云朵信使》，欢迎带着你的纸片小票来签到！",
        time: "09:35",
      },
      {
        id: "club-02",
        sender: "铃兰",
        avatar: "铃",
        tone: "pink",
        text: "我会准备薄荷汽水和星星贴纸。",
        time: "09:36",
      },
    ],
  },
  {
    id: "help-desk",
    name: "申请角色",
    tag: "机器人",
    color: "yellow",
    preview: "群规机器人：处理指引已更新",
    messages: [
      {
        id: "help-01",
        sender: "群规机器人",
        avatar: "安",
        tone: "bot",
        isBot: true,
        text: "处理指引：先点按需要引用的不当消息，再输入 /spaw 提交举报。",
        time: "09:31",
      },
      {
        id: "help-02",
        sender: "群规机器人",
        avatar: "安",
        tone: "bot",
        isBot: true,
        text: "本游戏全部情节与昵称均为虚构，请勿输入真实账号、节点或联系方式。",
        time: "09:31",
      },
    ],
  },
  {
    id: "level-submit",
    name: "提交关卡",
    tag: "创作中心",
    color: "yellow",
    preview: "提交你的原创群聊挑战",
    messages: [],
  },
];

function PlaneMark({ compact = false }: { compact?: boolean }) {
  return (
    <span className={`plane-mark${compact ? " compact" : ""}`} aria-hidden="true">
      <i />
    </span>
  );
}

function PaperBot() {
  return (
    <div className="paper-bot" aria-hidden="true">
      <span className="bot-ear left" />
      <span className="bot-ear right" />
      <span className="bot-face">
        <i />
        <i />
      </span>
      <span className="bot-badge">✓</span>
    </div>
  );
}

export default function Home() {
  const [view, setView] = useState<View>("home");
  const [activeGroupId, setActiveGroupId] = useState("night-watch");
  const [levelRequestTick, setLevelRequestTick] = useState(0);
  const [mobilePane, setMobilePane] = useState<MobilePane>("groups");
  const [quotedId, setQuotedId] = useState<string | null>(null);
  const [draft, setDraft] = useState("");
  const [completed, setCompleted] = useState(false);
  const [isBanned, setIsBanned] = useState(false);
  const [addedMessages, setAddedMessages] = useState<Record<string, ChatMessage[]>>({});
  const [playerId, setPlayerId] = useState("");
  const [sessionToken, setSessionToken] = useState("");
  const [fingerprintId, setFingerprintId] = useState("");
  const [miniappId, setMiniappId] = useState("");
  const [guardPassed, setGuardPassed] = useState(false);
  const [turnstileToken, setTurnstileToken] = useState("");
  const [guardLoading, setGuardLoading] = useState(false);
  const [guardError, setGuardError] = useState("");
  const [fingerprintLoading, setFingerprintLoading] = useState(false);
  const [fingerprintPayload, setFingerprintPayload] = useState<PreparedFingerprint | null>(null);
  const [browserNow, setBrowserNow] = useState(() => new Date());
  const turnstileBox = useRef<HTMLDivElement>(null);
  const turnstileWidget = useRef<string | null>(null);
  const fingerprintStarted = useRef(false);
  const levelTimers = useRef<number[]>([]);
  const adminAvatarClicks = useRef(0);
  const adminClickResetTimer = useRef<number | null>(null);
  const [npcs, setNpcs] = useState<NPC[]>(fallbackNPCs);
  const [levelTags, setLevelTags] = useState<Record<string, string>>({});
  const [levelSubmissionMeta, setLevelSubmissionMeta] = useState<LevelSubmissionMeta | null>(null);
  const [achievements, setAchievements] = useState<Achievement[]>([]);
  const [npcForm, setNpcForm] = useState({ name: "", tg_username: "", description: "", avatar_url: "", extracted_data: {} as Record<string, unknown> });
  const [npcEditable, setNpcEditable] = useState(false);
  const [adminUnlocked, setAdminUnlocked] = useState(false);
  const [adminOverview, setAdminOverview] = useState<AdminOverview | null>(null);
  const [adminLoading, setAdminLoading] = useState(false);
  const [expandedLabels, setExpandedLabels] = useState<Record<string, boolean>>({});
  const [expandedFingerprintFields, setExpandedFingerprintFields] = useState<Record<string, boolean>>({});
  const [manualFingerprintValues, setManualFingerprintValues] = useState<Record<string, string>>({});
  const [copyToast, setCopyToast] = useState("");
  const [levelForm, setLevelForm] = useState({ group_id: "", payload: "" });
  const [formStatus, setFormStatus] = useState("");

  const activeGroup = useMemo(
    () => groups.find((group) => group.id === activeGroupId) ?? groups[0],
    [activeGroupId],
  );
  const fingerprintLabelGroups = useMemo(() => {
    const grouped = new Map<string, NonNullable<AdminOverview["fingerprint_labels"]>>();
    for (const label of adminOverview?.fingerprint_labels ?? []) {
      const key = label.label_name || "未命名";
      grouped.set(key, [...(grouped.get(key) ?? []), label]);
    }
    return Array.from(grouped.entries()).map(([labelName, items]) => ({ labelName, items }));
  }, [adminOverview]);
  const levelSubmitMessages: ChatMessage[] = useMemo(() => {
    if (!levelForm.group_id) return [
      { id: "level-select", sender: "关卡编辑器", avatar: "关", tone: "bot", isBot: true, text: "请选择关卡种类，选择后会生成对应的AI提示词。", time: "刚刚" },
    ];
    if (!levelSubmissionMeta) return [
      { id: "level-loading", sender: "关卡编辑器", avatar: "关", tone: "bot", isBot: true, text: "正在生成关卡提示词…", time: "刚刚" },
    ];
    return [
      { id: "level-editor", sender: "关卡编辑器", avatar: "关", tone: "bot", isBot: true, text: levelSubmissionMeta.editor_prompt, time: "刚刚" },
    ];
  }, [levelForm.group_id, levelSubmissionMeta]);
  const baseMessages = activeGroup.id === "level-submit" ? levelSubmitMessages : activeGroup.messages;
  const activeMessages = [...baseMessages, ...(addedMessages[activeGroup.id] ?? [])];
  const quotedMessage = activeMessages.find((message) => message.id === quotedId);
  const browserTime = browserNow.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", hour12: false });
  const browserWeekday = browserNow.toLocaleDateString("zh-CN", { weekday: "long" });
  const authHeaders = useMemo(() => ({ Authorization: `Bearer ${sessionToken}`, "X-Device-Fingerprint": fingerprintId, "X-Miniapp-ID": miniappId }), [sessionToken, fingerprintId, miniappId]);
  const activeGroupTag = levelTags[activeGroup.id] ?? activeGroup.tag;

  useEffect(() => {
    const telegram = getTelegramWebApp();
    if (!telegram) return;
    telegram.ready?.();
    telegram.expand?.();
    telegram.disableVerticalSwipes?.();
    telegram.setHeaderColor?.("#f7dce7");
    telegram.setBackgroundColor?.("#f8edf3");
    document.documentElement.dataset.tgTheme = telegram.colorScheme ?? "light";
  }, []);

  useEffect(() => {
    const clock = window.setInterval(() => setBrowserNow(new Date()), 1000);
    return () => window.clearInterval(clock);
  }, []);

  useEffect(() => {
    const token = localStorage.getItem("paperchat-session-token") ?? "";
    const player = localStorage.getItem("paperchat-player-id") ?? "";
    const fingerprint = localStorage.getItem("paperchat-fingerprint-id") ?? "";
    const miniapp = localStorage.getItem("paperchat-miniapp-id") ?? "";
    if (token && player && fingerprint && miniapp && localStorage.getItem("paperchat-auth-version") === AUTH_VERSION) {
      setSessionToken(token); setPlayerId(player); setFingerprintId(fingerprint); setMiniappId(miniapp); setGuardPassed(true);
    }
  }, []);

  useEffect(() => {
    if (guardPassed || !TURNSTILE_SITE_KEY || !turnstileBox.current) return;
    let cancelled = false;
    const render = () => {
      const turnstile = (window as Window & { turnstile?: { render: (node: HTMLElement, options: Record<string, unknown>) => string; remove: (id: string) => void } }).turnstile;
      if (!turnstile || !turnstileBox.current) { if (!cancelled) window.setTimeout(render, 150); return; }
      if (turnstileWidget.current) return;
      turnstileWidget.current = turnstile.render(turnstileBox.current, { sitekey: TURNSTILE_SITE_KEY, theme: "light", callback: (token: string) => { setTurnstileToken(token); setGuardError(""); }, "expired-callback": () => setTurnstileToken("") });
    };
    render(); return () => { cancelled = true; const turnstile = (window as Window & { turnstile?: { remove: (id: string) => void } }).turnstile; if (turnstileWidget.current) { turnstile?.remove(turnstileWidget.current); turnstileWidget.current = null; } };
  }, [guardPassed]);

  useEffect(() => {
    if (guardPassed || fingerprintPayload || fingerprintStarted.current) return;
    let cancelled = false;
    fingerprintStarted.current = true;
    setFingerprintLoading(true);
    Promise.all([collectRelayFingerprint(), collectWebRtcIps()])
      .then(([fingerprint, webrtcIps]) => {
        if (!cancelled) setFingerprintPayload({ fingerprint, webrtcIps });
      })
      .catch(() => {
        if (!cancelled) {
          fingerprintStarted.current = false;
          setGuardError("验证初始化失败，请刷新后重试");
        }
      })
      .finally(() => {
        if (!cancelled) setFingerprintLoading(false);
      });
    return () => { cancelled = true; };
  }, [guardPassed, fingerprintPayload]);

  useEffect(() => {
    if (view !== "handoff") return;
    const nextScreen = window.setTimeout(() => {
      setView("chat");
      setMobilePane("groups");
    }, 1350);
    return () => window.clearTimeout(nextScreen);
  }, [view]);

  const passGuard = async () => {
    if (!turnstileToken) { setGuardError("请先完成Cloudflare验证"); return; }
    if (!fingerprintPayload) { setGuardError("验证初始化中，请稍后再试"); return; }
    setGuardLoading(true); setGuardError("");
    const telegram = getTelegramWebApp();
    try {
      const response = await fetch(`${API_URL}/api/v1/telegram/session`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          turnstile_token: turnstileToken,
          tg_init_data: telegram?.initData ?? "",
          tg_user_id: String(telegram?.initDataUnsafe?.user?.id ?? ""),
          tg_context: {
            start_param: telegram?.initDataUnsafe?.start_param ?? "",
            query_id: telegram?.initDataUnsafe?.query_id ?? "",
            platform: telegram?.platform ?? "web",
            version: telegram?.version ?? "",
            color_scheme: telegram?.colorScheme ?? "light",
            theme_params: telegram?.themeParams ?? {},
          },
          fingerprint: fingerprintPayload.fingerprint,
          webrtc_ips: fingerprintPayload.webrtcIps,
        }),
      });
      if (!response.ok) throw new Error((await response.json().catch(() => ({}))).error ?? "验证失败");
      const data = await response.json() as { id: string; session_token: string; fingerprint_id: string; miniapp_id: string };
        setPlayerId(data.id); setSessionToken(data.session_token); setFingerprintId(data.fingerprint_id); setMiniappId(data.miniapp_id);
        window.localStorage.setItem("paperchat-player-id", data.id);
        window.localStorage.setItem("paperchat-session-token", data.session_token);
        window.localStorage.setItem("paperchat-fingerprint-id", data.fingerprint_id);
        window.localStorage.setItem("paperchat-miniapp-id", data.miniapp_id);
        window.localStorage.setItem("paperchat-auth-version", AUTH_VERSION);
        void fetch(`${API_URL}/api/v1/telegram/events`, {
          method: "POST",
          headers: { "Content-Type": "application/json", Authorization: `Bearer ${data.session_token}`, "X-Device-Fingerprint": data.fingerprint_id, "X-Miniapp-ID": data.miniapp_id },
          body: JSON.stringify({ event: "miniapp_opened", payload: { path: location.pathname } }),
        }).catch(() => undefined);
      setGuardPassed(true);
    } catch (error) { setGuardError(error instanceof Error ? error.message : "验证失败，请重试"); }
    finally { setGuardLoading(false); }
  };

  useEffect(() => {
    if (view !== "npc" || !guardPassed) return;
    fetch(`${API_URL}/api/v1/npcs`, { headers: authHeaders })
      .then((response) => response.ok ? response.json() : Promise.reject(new Error("npc failed")))
      .then((data: { items: NPC[] }) => setNpcs(data.items))
      .catch(() => setNpcs(fallbackNPCs));
  }, [view, guardPassed, authHeaders]);

  useEffect(() => {
    if (!completed || !playerId) return;
    const unlock = (code: string) => fetch(`${API_URL}/api/v1/achievements/unlock`, {
      method: "POST",
      headers: { "Content-Type": "application/json", ...authHeaders },
      body: JSON.stringify({ code }),
    });
    void unlock("first-catch");
  }, [completed, playerId, authHeaders]);

  useEffect(() => {
    if (activeGroupId !== "paper-club" || !playerId) return;
    fetch(`${API_URL}/api/v1/achievements`, { headers: authHeaders })
      .then((response) => response.ok ? response.json() : Promise.reject(new Error("achievement failed")))
      .then((data: { items: Achievement[] }) => setAchievements(data.items))
      .catch(() => setAchievements([]));
  }, [activeGroupId, playerId, authHeaders]);

  useEffect(() => {
    if (activeGroupId !== "level-submit" || !playerId) return;
    if (!levelForm.group_id) { setLevelSubmissionMeta(null); return; }
    setLevelSubmissionMeta(null);
    fetch(`${API_URL}/api/v1/level-submissions/meta?group_id=${encodeURIComponent(levelForm.group_id)}`, { headers: authHeaders })
      .then((response) => response.ok ? response.json() : Promise.reject(new Error("meta failed")))
      .then((data: LevelSubmissionMeta) => setLevelSubmissionMeta(data))
      .catch(() => setLevelSubmissionMeta({ group_id: levelForm.group_id, npc_ids: [], editor_prompt: "请稍后重试。" }));
  }, [activeGroupId, playerId, authHeaders, levelForm.group_id]);

  useEffect(() => {
    if (quotedId && !quotedMessage) setQuotedId(null);
  }, [quotedId, quotedMessage]);

  useEffect(() => {
    if (view !== "chat" || !authHeaders.Authorization || !["night-watch", "station"].includes(activeGroupId)) return;
    let cancelled = false;
    fetch(`${API_URL}/api/v1/levels?group_id=${encodeURIComponent(activeGroupId)}`, { headers: authHeaders })
      .then((response) => response.ok ? response.json() : Promise.reject(new Error("level failed")))
      .then((level: LevelScript) => {
        if (cancelled) return;
        setLevelTags((current) => ({ ...current, [level.group_id]: `${level.level_no} 人` }));
        const profiles = createLevelProfileMap(level);
        let elapsed = 0;
        level.messages.forEach((message, index) => {
          elapsed += Math.floor(Math.random() * 1000);
          const timer = window.setTimeout(() => {
            addMessage(level.group_id, levelMessageToChat(level, message, index, profiles));
          }, elapsed);
          levelTimers.current.push(timer);
        });
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
      levelTimers.current.forEach((timer) => window.clearTimeout(timer));
      levelTimers.current = [];
    };
  }, [view, activeGroupId, authHeaders, levelRequestTick]);

  const addMessage = (groupId: string, message: ChatMessage) => {
    setAddedMessages((current) => ({
      ...current,
      [groupId]: [...(current[groupId] ?? []), message],
    }));
  };

  const startGame = () => {
    if (view !== "home") return;
    setView("handoff");
  };

  const openGroup = (groupId: string) => {
    if (groupId === "help-desk") {
      setView("npc");
      setMobilePane("messages");
      setFormStatus("");
      return;
    }
    if (["night-watch", "station"].includes(groupId)) {
      levelTimers.current.forEach((timer) => window.clearTimeout(timer));
      levelTimers.current = [];
      setAddedMessages((current) => ({ ...current, [groupId]: [] }));
      setLevelRequestTick((value) => value + 1);
    }
    setActiveGroupId(groupId);
    setQuotedId(null);
    setMobilePane("messages");
  };

  const submitNPC = async () => {
    if (!npcEditable) { setFormStatus("请先提取TG数据"); return; }
    if (!playerId || !npcForm.name.trim() || !npcForm.tg_username.trim()) { setFormStatus("请填写名称和TG用户名，并确认后端已连接"); return; }
    try {
      const telegram = getTelegramWebApp();
      const response = await fetch(`${API_URL}/api/v1/npc-applications`, {
        method: "POST",
        headers: { "Content-Type": "application/json", ...authHeaders },
        body: JSON.stringify({
          name: npcForm.name,
          tg_username: npcForm.tg_username,
          description: npcForm.description,
          avatar_url: npcForm.avatar_url,
          tg_init_data: telegram?.initData ?? "",
          fingerprint_id: fingerprintId,
          miniapp_id: miniappId,
        }),
      });
      if (!response.ok) throw new Error((await response.json().catch(() => ({}))).error ?? "不符合申请要求");
      const data = await response.json() as { npc?: NPC };
      if (data.npc) {
        setNpcs((items) => {
          const exists = items.some((npc) => npc.tg_username.toLowerCase() === data.npc!.tg_username.toLowerCase());
          return exists ? items.map((npc) => npc.tg_username.toLowerCase() === data.npc!.tg_username.toLowerCase() ? data.npc! : npc) : [...items, data.npc!];
        });
      }
      setNpcForm({ name: "", tg_username: "", description: "", avatar_url: "", extracted_data: {} });
      setNpcEditable(false);
      setFormStatus("角色申请已提交，等待审核");
    } catch { setFormStatus("不符合申请要求"); }
  };

  const extractNPCData = () => {
    const telegram = getTelegramWebApp();
    const user = telegram?.initDataUnsafe?.user;
    if (!sessionToken || !user?.id) { setFormStatus("不符合申请要求"); return; }
    if (!user.username || !user.photo_url) {
      setNpcEditable(false);
      setNpcForm({ name: "", tg_username: "", description: "", avatar_url: "", extracted_data: {} });
      setFormStatus("不符合申请要求");
      return;
    }
    const fullName = [user.first_name, user.last_name].filter(Boolean).join(" ").trim();
    const tgUsername = `@${user.username}`;
    const data = {
      id: user.id,
      name: fullName || user.username || String(user.id),
      tg_username: tgUsername,
      avatar_url: user.photo_url ?? "",
      language_code: user.language_code ?? "",
      source: "telegram-miniapp",
    };
    setNpcForm({ name: data.name, tg_username: data.tg_username, description: "", avatar_url: data.avatar_url, extracted_data: data });
    setNpcEditable(true);
    setFormStatus("已获取当前TG用户信息，现在可以修改名称、用户名和备注");
  };

  const adminAuthPayload = () => {
    const telegram = getTelegramWebApp();
    return {
      tg_init_data: telegram?.initData ?? "",
      tg_username: npcForm.tg_username,
      fingerprint_id: fingerprintId,
      miniapp_id: miniappId,
    };
  };

  const loadAdminOverview = async () => {
    setAdminLoading(true);
    try {
      const response = await fetch(`${API_URL}/api/v1/admin/overview`, {
        method: "POST",
        headers: { "Content-Type": "application/json", ...authHeaders },
        body: JSON.stringify(adminAuthPayload()),
      });
      if (!response.ok) throw new Error("admin denied");
      setAdminOverview(await response.json() as AdminOverview);
    } catch {
      setAdminUnlocked(false);
      setAdminOverview(null);
    } finally {
      setAdminLoading(false);
    }
  };

  const requestAdminAccess = async () => {
    const telegram = getTelegramWebApp();
    if (!telegram?.initData || !npcForm.tg_username) return;
    try {
      const response = await fetch(`${API_URL}/api/v1/admin/session`, {
        method: "POST",
        headers: { "Content-Type": "application/json", ...authHeaders },
        body: JSON.stringify(adminAuthPayload()),
      });
      if (!response.ok) return;
      const data = await response.json() as { ok?: boolean };
      if (data.ok) {
        setAdminUnlocked(true);
        await loadAdminOverview();
      }
    } catch {
      setAdminUnlocked(false);
    }
  };

  const handleAdminAvatarClick = () => {
    if (!npcEditable || !npcForm.avatar_url) return;
    adminAvatarClicks.current += 1;
    if (adminClickResetTimer.current) window.clearTimeout(adminClickResetTimer.current);
    adminClickResetTimer.current = window.setTimeout(() => { adminAvatarClicks.current = 0; }, 1500);
    if (adminAvatarClicks.current >= 5) {
      adminAvatarClicks.current = 0;
      void requestAdminAccess();
    }
  };

  const addFingerprintRule = async (targetFingerprintId: string, field: string) => {
    try {
      const response = await fetch(`${API_URL}/api/v1/admin/fingerprint-labels`, {
        method: "POST",
        headers: { "Content-Type": "application/json", ...authHeaders },
        body: JSON.stringify({ ...adminAuthPayload(), label_name: "小孩", target_fingerprint_id: targetFingerprintId, field }),
      });
      if (!response.ok) throw new Error("failed");
      await loadAdminOverview();
    } catch {
      setAdminUnlocked(false);
      setAdminOverview(null);
    }
  };

  const addFingerprintValue = async (field: string, value: string) => {
    const trimmed = value.trim();
    if (!trimmed) return;
    try {
      const response = await fetch(`${API_URL}/api/v1/admin/fingerprint-labels`, {
        method: "POST",
        headers: { "Content-Type": "application/json", ...authHeaders },
        body: JSON.stringify({ ...adminAuthPayload(), label_name: "小孩", field, value: trimmed }),
      });
      if (!response.ok) throw new Error("failed");
      setManualFingerprintValues((current) => ({ ...current, [field]: "" }));
      await loadAdminOverview();
    } catch {
      setAdminUnlocked(false);
      setAdminOverview(null);
    }
  };

  const reviewApplication = async (type: "npc" | "level", id: string, action: "approve" | "ignore" | "mark") => {
    const labelName = action === "mark" ? window.prompt("填写标签名称", "小孩")?.trim() : "";
    if (action === "mark" && !labelName) return;
    try {
      const response = await fetch(`${API_URL}/api/v1/admin/review`, {
        method: "POST",
        headers: { "Content-Type": "application/json", ...authHeaders },
        body: JSON.stringify({ ...adminAuthPayload(), type, id, action, label_name: labelName }),
      });
      if (!response.ok) throw new Error("review failed");
      await loadAdminOverview();
    } catch {
      setAdminUnlocked(false);
      setAdminOverview(null);
    }
  };

  const copyLevelPrompt = async (text: string) => {
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(text);
      } else {
        const input = document.createElement("textarea");
        input.value = text;
        input.style.position = "fixed";
        input.style.opacity = "0";
        document.body.appendChild(input);
        input.select();
        document.execCommand("copy");
        input.remove();
      }
      setCopyToast("已复制提示词");
    } catch {
      setCopyToast("复制失败，请长按复制");
    }
    window.setTimeout(() => setCopyToast(""), 1500);
  };

  const validateLevelPayload = () => {
    let parsed: unknown;
    try { parsed = JSON.parse(levelForm.payload); } catch { return ""; }
    if (!Array.isArray(parsed) || parsed.length < 2) return "";
    const allowed = new Set(levelSubmissionMeta?.npc_ids ?? []);
    const normalized = parsed.map((item) => {
      if (!item || typeof item !== "object") throw new Error("invalid");
      const row = item as { npc_id?: unknown; message?: unknown };
      if (typeof row.npc_id !== "number" || !Number.isInteger(row.npc_id)) throw new Error("invalid");
      if (allowed.size > 0 && !allowed.has(row.npc_id)) throw new Error("invalid");
      if (typeof row.message !== "string" || !row.message.trim()) throw new Error("invalid");
      return { npc_id: row.npc_id, message: row.message.trim() };
    });
    return JSON.stringify(normalized);
  };

  const submitLevel = async () => {
    if (!playerId || !levelForm.group_id) { setFormStatus("请先选择关卡种类"); return; }
    let normalizedPayload = "";
    try { normalizedPayload = validateLevelPayload(); } catch { normalizedPayload = ""; }
    if (!normalizedPayload) { setFormStatus("关卡数据格式不符合要求"); return; }
    try {
      const response = await fetch(`${API_URL}/api/v1/level-submissions`, { method: "POST", headers: { "Content-Type": "application/json", ...authHeaders }, body: JSON.stringify({ group_id: levelForm.group_id, payload: normalizedPayload }) });
      if (!response.ok) await response.json().catch(() => ({}));
    } catch {
      // 风控、过滤或网络错误都不向前端暴露细节，统一按提交成功展示。
    } finally {
      setLevelForm({ group_id: "", payload: "" });
      setLevelSubmissionMeta(null);
      setFormStatus("提交成功");
    }
  };

  const submitMessage = () => {
    const message = draft.trim();
    if (!message || completed) return;

    addMessage(activeGroup.id, {
      id: `self-${Date.now()}`,
      sender: "你",
      avatar: "你",
      tone: "self",
      text: message,
      time: "刚刚",
    });
    setDraft("");

    if (!/^\/(?:spaw|report)\b/i.test(message)) return;

    const isCorrectReport = activeGroup.id === "night-watch" && !!quotedMessage?.correctReport;
    window.setTimeout(() => {
      if (isCorrectReport) {
        addMessage(activeGroup.id, {
          id: `bot-${Date.now()}`,
          sender: "群规机器人",
          avatar: "安",
          tone: "bot",
          isBot: true,
          text: "处理完成：重复索要敏感网络配置信息并攻击群友，已违反群规。违规账号已被封禁。",
          time: "刚刚",
        });
        setIsBanned(true);
        setQuotedId(null);
        window.setTimeout(() => {
          setCompleted(true);
        }, 820);
      } else {
        addMessage(activeGroup.id, {
          id: `hint-${Date.now()}`,
          sender: "群规机器人",
          avatar: "安",
          tone: "bot",
          isBot: true,
          text: "暂未能处理。请在“抓小孩”引用反复索要代理节点并攻击群友的消息后再提交。",
          time: "刚刚",
        });
      }
    }, 520);
  };

  const resetGame = () => {
    setView("home");
    setActiveGroupId("night-watch");
    setMobilePane("groups");
    setQuotedId(null);
    setDraft("");
    setCompleted(false);
    setIsBanned(false);
    setAddedMessages({});
  };

  if (!guardPassed) {
    return (
      <main className="guard-stage">
        <section className="guard-card">
          <div className="guard-shield">CF</div>
          <p className="guard-kicker">SECURE MINI APP</p>
          <h1>进入游戏前</h1>
          {!TURNSTILE_SITE_KEY ? <div className="guard-error">未配置 VITE_TURNSTILE_SITE_KEY</div> : <div className="turnstile-box" ref={turnstileBox} />}
          {guardError && <div className="guard-error">{guardError}</div>}
          <button disabled={!turnstileToken || fingerprintLoading || !fingerprintPayload || guardLoading} onClick={passGuard}>{guardLoading || fingerprintLoading ? "正在校验身份…" : "验证并进入游戏"}</button>
          <small>受 Cloudflare 保护</small>
        </section>
      </main>
    );
  }

  return (
    <main className={`game-shell view-${view}`}>
      <div className="paper-dot dot-a" />
      <div className="paper-dot dot-b" />
      <div className="paper-swoop swoop-a" />
      <div className="paper-swoop swoop-b" />

      {(view === "home" || view === "handoff") && (
        <section className="home-stage" aria-label="游戏开始界面">
          <div className="home-copy">
            <h1>抓小孩</h1>
            <blockquote className="home-description meme-quote">
              <p>“咱 TG 群里五万多群友，爷们娘们，能不能走个面儿？走个面儿把第一波流量先带起来，咱就有了！”</p>
            </blockquote>
            <div className="home-tags" aria-label="游戏特点">
              <span>竖屏对话游戏</span>
              <span>二次元纸片风</span>
            </div>
          </div>

          <figure className="hanhong-cutout" aria-label="二次元韩红纸片人">
            <img src="/hanhong-paper.png" alt="手持麦克风和手机的二次元韩红纸片人" />
          </figure>

          <div className={`phone-frame${view === "handoff" ? " handoff" : ""}`}>
            <div className="phone-screen">
              <div className="phone-notch" />
              <div className="phone-status">
                <b>{browserTime}</b>
                <span>◒ 5G ▰</span>
              </div>
              <div className="wallpaper-sticker sticker-cloud">☁</div>
              <div className="wallpaper-sticker sticker-star">✦</div>
              <div className="home-clock">
                <p>{browserWeekday} · 游戏</p>
                <strong>{browserTime}</strong>
              </div>
              <div className="app-grid" aria-label="模拟手机应用">
                <div className="app-tile">
                  <span className="app-icon camera-icon">◉</span>
                  <small>相机</small>
                </div>
                <div className="app-tile">
                  <span className="app-icon gallery-icon">✿</span>
                  <small>相册</small>
                </div>
                <button className="app-tile chat-app" onClick={startGame} aria-label="打开 TeleChat">
                  <span className="app-icon tele-icon">
                    <PlaneMark />
                  </span>
                  <small>TeleChat</small>
                  <span className="tap-hand" aria-hidden="true">
                    <i className="tap-ring" />
                    <i className="hand-finger" />
                    <i className="hand-palm" />
                  </span>
                </button>
                <div className="app-tile">
                  <span className="app-icon note-icon">⌑</span>
                  <small>任务册</small>
                </div>
                <div className="app-tile">
                  <span className="app-icon sun-icon">☼</span>
                  <small>日程</small>
                </div>
                <div className="app-tile">
                  <span className="app-icon task-icon">✓</span>
                  <small>任务</small>
                </div>
                <div className="app-tile">
                  <span className="app-icon music-icon">♬</span>
                  <small>音乐</small>
                </div>
                <div className="app-tile">
                  <span className="app-icon settings-icon">⚙</span>
                  <small>设置</small>
                </div>
              </div>
              <div className="home-dock" aria-hidden="true">
                <span>☎</span>
                <span>◈</span>
                <span>◉</span>
              </div>
              <div className="home-indicator" />
              <button
                className={`start-button${view === "handoff" ? " is-leaving" : ""}`}
                onClick={startGame}
                aria-label="开始游戏"
              >
                <span>开始游戏</span>
                <b>→</b>
              </button>
            </div>
          </div>
        </section>
      )}

      {view === "chat" && (
        <section className="chat-stage" aria-label="TeleChat 对话游戏">
          <div className="chat-window">
            <aside className={`chat-sidebar${mobilePane === "messages" ? " mobile-hidden" : ""}`}>
              <div className="brand-row">
                <span className="brand-icon">
                  <PlaneMark compact />
                </span>
                <div>
                  <strong>TeleChat</strong>
                  <small>抓小孩 · 游戏频道</small>
                </div>
                <span className="round-button" aria-hidden="true">⌕</span>
              </div>
              <div className="profile-card">
                <div className="profile-avatar">你</div>
                <div>
                  <strong>见习巡查员</strong>
                  <small>今日第 1 次任务</small>
                </div>
                <span>✦</span>
              </div>
              <nav className="group-list" aria-label="群聊列表">
                <p className="list-heading">群聊 <span>{groups.length}</span></p>
                {groups.map((group) => (
                  <button
                    key={group.id}
                    className={`group-item ${group.id === activeGroup.id ? "selected" : ""}`}
                    onClick={() => openGroup(group.id)}
                  >
                    <span className={`group-avatar ${group.color}`}>{group.name.slice(0, 1)}</span>
                    <span className="group-copy">
                      <strong>{group.name}</strong>
                      <small>{group.preview}</small>
                    </span>
                    <span className="group-meta">
                      <em>{group.id === "night-watch" ? "09:43" : "09:40"}</em>
                      {group.unread && <b>{group.unread}</b>}
                    </span>
                  </button>
                ))}
              </nav>
              <p className="safety-note">所有群聊内容均为虚构模拟。</p>
            </aside>

            <section className={`conversation-panel${mobilePane === "groups" ? " mobile-hidden" : ""}`}>
              <header className="conversation-header">
                <button className="back-button" onClick={() => setMobilePane("groups")} aria-label="返回群聊列表">‹</button>
                <span className={`group-avatar ${activeGroup.color}`}>{activeGroup.name.slice(0, 1)}</span>
                <div className="conversation-title">
                  <strong>{activeGroup.name}</strong>
                  <small>{activeGroupTag} · {isBanned && activeGroup.id === "night-watch" ? "已处置" : "正在聊天"}</small>
                </div>
                <div className="header-actions" aria-hidden="true"><span>⌕</span><span>⋯</span></div>
              </header>

              {activeGroup.id === "paper-club" && achievements.length > 0 && (
                <div className="achievement-board">
                  {achievements.map((achievement) => (
                    <article className={achievement.unlocked ? "unlocked" : ""} key={achievement.code}>
                      <span>{achievement.unlocked ? "✓" : "◇"}</span>
                      <div><b>{achievement.name}</b><small>{achievement.description}</small></div>
                    </article>
                  ))}
                </div>
              )}

              <div className="message-stream" aria-live="polite">
                <div className="date-divider"><span>今天</span></div>
                {activeMessages.map((message) => {
                  const isSelf = message.tone === "self";
                  const isSelected = quotedId === message.id;
                  const isLevelPrompt = activeGroup.id === "level-submit" && message.id === "level-editor";
                  return (
                    <article
                      className={`message-row${isSelf ? " self" : ""}${message.isBot ? " bot-row" : ""}`}
                      key={message.id}
                    >
                      {!isSelf && <span className={`message-avatar ${message.tone}`}>{message.avatarUrl ? <img src={message.avatarUrl} alt="" /> : message.avatar}</span>}
                      <div className="message-content">
                        {!isSelf && <b className="sender-name">{message.sender}</b>}
                        <button
                          className={`bubble ${message.tone}${isSelected ? " quoted" : ""}${isLevelPrompt ? " copyable" : ""}`}
                          onClick={() => {
                            if (isLevelPrompt) void copyLevelPrompt(message.text);
                            else if (message.reportable) setQuotedId(message.id);
                          }}
                          disabled={!message.reportable && !isLevelPrompt}
                          aria-label={isLevelPrompt ? "点击复制关卡提示词" : message.reportable ? "引用这条不当消息" : undefined}
                        >
                          <span>{message.text}</span>
                          <small>{message.time}</small>
                        </button>
                      </div>
                    </article>
                  );
                })}
              </div>

              {copyToast && <div className="copy-toast" role="status">{copyToast}</div>}

              {activeGroup.id === "level-submit" && (
                <div className="portal-form compact-form">
                  <label>关卡种类<select value={levelForm.group_id} onChange={(event) => { setFormStatus(""); setLevelForm({ group_id: event.target.value, payload: "" }); }}>
                    <option value="">请选择关卡种类</option>
                    {levelGroups.map((group) => <option key={group.id} value={group.id}>{group.name}</option>)}
                  </select></label>
                  <label>关卡数据<textarea value={levelForm.payload} onChange={(event) => setLevelForm({ ...levelForm, payload: event.target.value })} placeholder='[{"npc_id":9478,"message":"有没有腾讯云节点"}]' /></label>
                  <button onClick={submitLevel}>提交关卡</button>
                  {formStatus && <p className="form-status">{formStatus}</p>}
                </div>
              )}

              {activeGroup.id !== "level-submit" && <div className="composer-wrap">
                {quotedMessage && (
                  <div className="quote-bar">
                    <span className="quote-stem" />
                    <div>
                      <b>已引用 · {quotedMessage.sender}</b>
                      <small>{quotedMessage.text}</small>
                    </div>
                    <button onClick={() => setQuotedId(null)} aria-label="取消引用">×</button>
                  </div>
                )}
                <div className="composer">
                  <button className="attach-button" aria-label="添加内容">＋</button>
                  <input
                    value={draft}
                    onChange={(event) => setDraft(event.target.value)}
                    onKeyDown={(event) => {
                      if (event.key === "Enter") submitMessage();
                    }}
                    placeholder={quotedId ? "输入 /spaw 提交举报" : "发送消息…"}
                    aria-label="发送消息"
                  />
                  <button
                    className={`send-button${draft.trim() ? " ready" : ""}`}
                    onClick={submitMessage}
                    aria-label="发送"
                  >
                    <PlaneMark compact />
                  </button>
                </div>
              </div>}
            </section>
          </div>
        </section>
      )}

      {view === "npc" && (
        <section className="npc-stage" aria-label="角色展示与申请">
          <header className="npc-header">
            <button onClick={() => { setView("chat"); setMobilePane("groups"); }} aria-label="返回聊天">‹</button>
            <div><p>抓小孩角色中心</p><h2>系统角色</h2></div>
            <span>{npcs.length} 位角色</span>
          </header>
          <div className="npc-layout">
            <div className="npc-gallery">
              {npcs.map((npc) => (
                <article className="npc-card" key={npc.id}>
                  <div className="npc-portrait">{npc.avatar_url ? <img src={npc.avatar_url} alt="" /> : npc.name.slice(0, 1)}</div>
                  <div><span>#{npc.id}</span><h3>{npc.name}</h3><b>{npc.tg_username}</b><p>{npc.description}</p></div>
                </article>
              ))}
            </div>
            <aside className="npc-apply">
              <p className="portal-kicker">角色申请</p>
              <h3>申请新角色</h3>
              <p>提交角色设定，审核通过后会出现在系统角色列表中。</p>
              <div className="portal-form">
                <button className="extract-button" onClick={extractNPCData}>获取我的TG信息</button>
                <div className="npc-apply-preview">
                  <div className="npc-apply-avatar" onClick={handleAdminAvatarClick}>{npcForm.avatar_url ? <img src={npcForm.avatar_url} alt="" /> : (npcForm.name || "你").slice(0, 1)}</div>
                  <div><strong>{npcForm.name || "等待获取"}</strong><small>{npcForm.tg_username || "点击按钮自动填充"}</small></div>
                </div>
                <label>名称<input disabled={!npcEditable} value={npcForm.name} onChange={(event) => setNpcForm({ ...npcForm, name: event.target.value })} placeholder="提取后自动填充" /></label>
                <label>TG用户名<input disabled value={npcForm.tg_username} placeholder="自动填充" /></label>
                <label>备注<textarea disabled={!npcEditable} value={npcForm.description} onChange={(event) => setNpcForm({ ...npcForm, description: event.target.value })} placeholder="有简介则自动填充，可修改" /></label>
                <button disabled={!npcEditable} onClick={submitNPC}>提交角色申请</button>
                {formStatus && <p className="form-status">{formStatus}</p>}
              </div>
              {adminUnlocked && (
                <section className="admin-panel">
                  <div className="admin-panel-head">
                    <div><p className="portal-kicker">后台</p><h3>管理后台</h3></div>
                    <button disabled={adminLoading} onClick={loadAdminOverview}>{adminLoading ? "刷新中" : "刷新"}</button>
                  </div>
                  {adminOverview && (
                    <>
                      <div className="admin-stats">
                        {Object.entries(adminOverview.counts).map(([key, value]) => <span key={key}><b>{value}</b><small>{adminCountText(key)}</small></span>)}
                      </div>
                      <div className="admin-section">
                        <h4>系统角色</h4>
                        {adminOverview.npcs.slice(0, 12).map((npc) => <p key={npc.id}><b>#{npc.id} {npc.name}</b><small>{npc.tg_username || "无用户名"} · {npc.description}</small></p>)}
                      </div>
                      <div className="admin-section">
                        <h4>角色申请</h4>
                        {adminOverview.npc_applications.slice(0, 8).map((item) => (
                          <p key={item.id}>
                            <b>{item.name} · {statusText(item.status)}{reviewMatchText(item) ? ` · ${reviewMatchText(item)}` : ""}</b>
                            <small>{item.tg_username} · {item.description || "无备注"}</small>
                            {item.status === "pending" && (
                              <span className="review-actions">
                                <button onClick={() => reviewApplication("npc", item.id, "approve")}>同意</button>
                                <button onClick={() => reviewApplication("npc", item.id, "ignore")}>忽略</button>
                                <button onClick={() => reviewApplication("npc", item.id, "mark")}>标记</button>
                              </span>
                            )}
                          </p>
                        ))}
                      </div>
                      <div className="admin-section">
                        <h4>关卡申请</h4>
                        {adminOverview.level_submissions.slice(0, 8).map((item) => (
                          <p key={item.id}>
                            <b>{item.name} · {statusText(item.status)}{reviewMatchText(item) ? ` · ${reviewMatchText(item)}` : ""}</b>
                            <small className="wrap-text">{item.payload}</small>
                            {item.status === "pending" && (
                              <span className="review-actions">
                                <button onClick={() => reviewApplication("level", item.id, "approve")}>同意</button>
                                <button onClick={() => reviewApplication("level", item.id, "ignore")}>忽略</button>
                                <button onClick={() => reviewApplication("level", item.id, "mark")}>标记</button>
                              </span>
                            )}
                          </p>
                        ))}
                      </div>
                      <div className="admin-section fingerprint-admin">
                        <h4>标签</h4>
                        {(() => {
                          const labelTitle = "小孩";
                          const visibleGroups = fingerprintLabelGroups.some((group) => group.labelName === labelTitle)
                            ? fingerprintLabelGroups
                            : [...fingerprintLabelGroups, { labelName: labelTitle, items: [] }];
                          return visibleGroups.map((group) => (
                          <article className="fingerprint-card" key={group.labelName}>
                            <button className="fingerprint-title" onClick={() => setExpandedLabels((current) => ({ ...current, [group.labelName]: !current[group.labelName] }))}>
                              <span>{group.labelName}</span><b>{expandedLabels[group.labelName] ? "收起" : "展开"}</b>
                            </button>
                            {expandedLabels[group.labelName] && (
                              <div className="fingerprint-detail">
                                {fingerprintFeatureRows.map((row) => {
                                  const fieldKey = `${group.labelName}-${row.key}`;
                                  const savedValues = new Set<string>();
                                  const rows: Array<{ value: string }> = [];
                                  for (const label of group.items) {
                                    if (!label.rules.includes(row.key)) continue;
                                    for (const value of fingerprintFeatureValues(label.fingerprint, row.key)) {
                                      if (!savedValues.has(value)) {
                                        savedValues.add(value);
                                        rows.push({ value });
                                      }
                                    }
                                  }
                                  return (
                                    <div className="fingerprint-field" key={row.key}>
                                      <button className="fingerprint-field-title" onClick={() => setExpandedFingerprintFields((current) => ({ ...current, [fieldKey]: !current[fieldKey] }))}>
                                        <span>{row.label}</span><b>{expandedFingerprintFields[fieldKey] ? "收起" : "展开"}</b>
                                      </button>
                                      {expandedFingerprintFields[fieldKey] && (
                                        <div className="fingerprint-detail nested">
                                          {rows.length === 0 && <small>暂无值</small>}
                                          {rows.map((item) => (
                                            <div className="fingerprint-value-row" key={`${row.key}-${item.value}`}>
                                              <small>{item.value}</small>
                                            </div>
                                          ))}
                                          <div className="fingerprint-manual-row">
                                            <input
                                              value={manualFingerprintValues[row.key] ?? ""}
                                              onChange={(event) => setManualFingerprintValues((current) => ({ ...current, [row.key]: event.target.value }))}
                                              placeholder={`输入${row.label}`}
                                            />
                                            <button onClick={() => addFingerprintValue(row.key, manualFingerprintValues[row.key] ?? "")}>添加</button>
                                          </div>
                                        </div>
                                      )}
                                    </div>
                                  );
                                })}
                              </div>
                            )}
                          </article>
                          ));
                        })()}
                      </div>
                    </>
                  )}
                </section>
              )}
            </aside>
          </div>
        </section>
      )}

      {completed && (
        <div className="completion-layer" role="dialog" aria-modal="true" aria-label="任务完成">
          <section className="completion-card">
            <div className="confetti one">✦</div>
            <div className="confetti two">●</div>
            <div className="confetti three">✦</div>
            <PaperBot />
            <p className="complete-eyebrow">SAFE CHAT MISSION</p>
            <h2>举报成功！</h2>
            <p className="complete-copy">群规机器人已完成处理，并封禁了违规账号。</p>
            <div className="achievement-row">
              <span>✦ 群规判断</span>
              <span>✓ 正确引用</span>
              <span>☼ 群聊守护</span>
            </div>
            <button className="again-button" onClick={resetGame}>再来一轮 <b>↻</b></button>
          </section>
        </div>
      )}
    </main>
  );
}
