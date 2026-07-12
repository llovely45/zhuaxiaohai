import { useEffect, useMemo, useState } from "react";

type View = "home" | "handoff" | "chat" | "npc";
type MobilePane = "groups" | "messages";

type ChatMessage = {
  id: string;
  sender: string;
  avatar: string;
  tone: "mint" | "pink" | "yellow" | "lilac" | "bot" | "self";
  text: string;
  time: string;
  reportable?: boolean;
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
type TelegramWebApp = {
  initData?: string;
  initDataUnsafe?: { user?: { id?: number }; start_param?: string; query_id?: string };
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
const fallbackNPCs: NPC[] = [
  { id: 0, name: "顶尖哥", tg_username: "@anlianxiaoliu", description: "他很顶尖。", avatar_url: "" },
  { id: 9478, name: "小孩哥", tg_username: "@xiaohai", description: "到处索要代理节点，要不到就开始嘴硬。", avatar_url: "" },
  { id: 1, name: "群规机器人", tg_username: "@thisisabot", description: "负责审核举报、封禁违规账号并发放成就。", avatar_url: "" },
];

function getTelegramWebApp(): TelegramWebApp | undefined {
  return (window as Window & { Telegram?: { WebApp?: TelegramWebApp } }).Telegram?.WebApp;
}

const groups: ChatGroup[] = [
  {
    id: "night-watch",
    name: "抓小孩",
    tag: "43 人",
    color: "pink",
    preview: "小孩哥：又来问节点了…",
    unread: 2,
    messages: [
      {
        id: "night-01",
        sender: "群规小卡",
        avatar: "规",
        tone: "yellow",
        text: "群规提醒：不索要代理节点、不刷屏，也不要攻击群友。",
        time: "09:42",
      },
      {
        id: "clue-01",
        sender: "小孩哥",
        avatar: "板",
        tone: "pink",
        text: "又有人有能用的代理节点吗？没有就别装懂。",
        time: "09:43",
        reportable: true,
      },
    ],
  },
  {
    id: "station",
    name: "胡说哥传奇",
    tag: "18 人",
    color: "mint",
    preview: "今天的纸片徽章已经发放～",
    messages: [
      {
        id: "station-01",
        sender: "小灯泡",
        avatar: "灯",
        tone: "mint",
        text: "今天的纸片徽章已经发放，记得查看自己的任务板。",
        time: "09:39",
      },
      {
        id: "station-02",
        sender: "路牌",
        avatar: "路",
        tone: "lilac",
        text: "群聊提示：不索要或分享代理节点，也不因得不到回复攻击他人。",
        time: "09:40",
      },
    ],
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
    name: "申请NPC",
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
    messages: [
      {
        id: "level-01",
        sender: "关卡编辑器",
        avatar: "关",
        tone: "bot",
        isBot: true,
        text: "写下关卡名称、玩法说明和关卡数据，即可提交审核。",
        time: "09:31",
      },
    ],
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

async function browserFingerprint() {
  const details = {
    userAgent: navigator.userAgent,
    language: navigator.language,
    platform: navigator.platform,
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    screen: `${screen.width}x${screen.height}x${screen.colorDepth}`,
    hardwareConcurrency: navigator.hardwareConcurrency,
    deviceMemory: (navigator as Navigator & { deviceMemory?: number }).deviceMemory,
  };
  const bytes = new TextEncoder().encode(JSON.stringify(details));
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  return { ...details, visitorId: Array.from(new Uint8Array(digest)).map((n) => n.toString(16).padStart(2, "0")).join("") };
}

export default function Home() {
  const [view, setView] = useState<View>("home");
  const [activeGroupId, setActiveGroupId] = useState("night-watch");
  const [mobilePane, setMobilePane] = useState<MobilePane>("groups");
  const [quotedId, setQuotedId] = useState<string | null>(null);
  const [draft, setDraft] = useState("");
  const [completed, setCompleted] = useState(false);
  const [isBanned, setIsBanned] = useState(false);
  const [addedMessages, setAddedMessages] = useState<Record<string, ChatMessage[]>>({});
  const [playerId, setPlayerId] = useState("");
  const [sessionToken, setSessionToken] = useState("");
  const [npcs, setNpcs] = useState<NPC[]>(fallbackNPCs);
  const [achievements, setAchievements] = useState<Achievement[]>([]);
  const [npcLookup, setNpcLookup] = useState("");
  const [npcForm, setNpcForm] = useState({ name: "", tg_username: "", description: "", extracted_data: {} as Record<string, unknown> });
  const [npcEditable, setNpcEditable] = useState(false);
  const [levelForm, setLevelForm] = useState({ name: "", description: "", payload: "" });
  const [formStatus, setFormStatus] = useState("");

  const activeGroup = useMemo(
    () => groups.find((group) => group.id === activeGroupId) ?? groups[0],
    [activeGroupId],
  );
  const activeMessages = [...activeGroup.messages, ...(addedMessages[activeGroup.id] ?? [])];

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
    if (view !== "handoff") return;
    const nextScreen = window.setTimeout(() => {
      setView("chat");
      setMobilePane("groups");
    }, 1350);
    return () => window.clearTimeout(nextScreen);
  }, [view]);

  useEffect(() => {
    if (view !== "chat" && view !== "npc") return;
    const existing = window.localStorage.getItem("paperchat-player-id");
    const existingToken = window.localStorage.getItem("paperchat-session-token");
    if (existing && existingToken) { setPlayerId(existing); setSessionToken(existingToken); return; }
    const telegram = getTelegramWebApp();
    browserFingerprint()
      .then((fingerprint) => fetch(`${API_URL}/api/v1/telegram/session`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
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
          fingerprint,
        }),
      }))
      .then((response) => response.ok ? response.json() : Promise.reject(new Error("session failed")))
      .then((data: { id: string; session_token: string }) => {
        setPlayerId(data.id); setSessionToken(data.session_token);
        window.localStorage.setItem("paperchat-player-id", data.id);
        window.localStorage.setItem("paperchat-session-token", data.session_token);
        void fetch(`${API_URL}/api/v1/telegram/events`, {
          method: "POST",
          headers: { "Content-Type": "application/json", Authorization: `Bearer ${data.session_token}` },
          body: JSON.stringify({ event: "miniapp_opened", payload: { path: location.pathname } }),
        });
      })
      .catch(() => setFormStatus("后端暂未连接，页面仍可预览"));
  }, [view]);

  useEffect(() => {
    if (view !== "npc") return;
    fetch(`${API_URL}/api/v1/npcs`)
      .then((response) => response.ok ? response.json() : Promise.reject(new Error("npc failed")))
      .then((data: { items: NPC[] }) => setNpcs(data.items))
      .catch(() => setNpcs(fallbackNPCs));
  }, [view]);

  useEffect(() => {
    if (!completed || !playerId) return;
    const unlock = (code: string) => fetch(`${API_URL}/api/v1/achievements/unlock`, {
      method: "POST",
      headers: { "Content-Type": "application/json", Authorization: `Bearer ${sessionToken}` },
      body: JSON.stringify({ code }),
    });
    void unlock("first-catch");
  }, [completed, playerId, sessionToken]);

  useEffect(() => {
    if (activeGroupId !== "paper-club" || !playerId) return;
    fetch(`${API_URL}/api/v1/achievements`, { headers: { Authorization: `Bearer ${sessionToken}` } })
      .then((response) => response.ok ? response.json() : Promise.reject(new Error("achievement failed")))
      .then((data: { items: Achievement[] }) => setAchievements(data.items))
      .catch(() => setAchievements([]));
  }, [activeGroupId, playerId, sessionToken]);

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
    setActiveGroupId(groupId);
    setQuotedId(null);
    setMobilePane("messages");
  };

  const submitNPC = async () => {
    if (!npcEditable) { setFormStatus("请先提取TG数据"); return; }
    if (!playerId || !npcForm.name.trim() || !npcForm.tg_username.trim()) { setFormStatus("请填写名称和TG用户名，并确认后端已连接"); return; }
    try {
      const response = await fetch(`${API_URL}/api/v1/npc-applications`, { method: "POST", headers: { "Content-Type": "application/json", Authorization: `Bearer ${sessionToken}` }, body: JSON.stringify(npcForm) });
      if (!response.ok) throw new Error("submit failed");
      setNpcLookup("");
      setNpcForm({ name: "", tg_username: "", description: "", extracted_data: {} });
      setNpcEditable(false);
      setFormStatus("NPC申请已提交，等待审核");
    } catch { setFormStatus("提交失败，请稍后重试"); }
  };

  const extractNPCData = async () => {
    if (!sessionToken || !npcLookup.trim()) { setFormStatus("请先输入TG用户名或t.me链接"); return; }
    setFormStatus("正在提取TG资料…");
    try {
      const response = await fetch(`${API_URL}/api/v1/telegram/extract-profile`, {
        method: "POST",
        headers: { "Content-Type": "application/json", Authorization: `Bearer ${sessionToken}` },
        body: JSON.stringify({ username: npcLookup }),
      });
      if (!response.ok) throw new Error("extract failed");
      const data = await response.json() as { name: string; tg_username: string; description: string; source: string };
      setNpcForm({ name: data.name, tg_username: data.tg_username, description: data.description ?? "", extracted_data: data });
      setNpcEditable(true);
      setFormStatus("提取完成，现在可以修改名称、用户名和备注");
    } catch {
      setNpcEditable(false);
      setFormStatus("无法提取：该账号可能未与机器人交互，或不是可访问的公开账号");
    }
  };

  const submitLevel = async () => {
    if (!playerId || !levelForm.name.trim() || !levelForm.description.trim()) { setFormStatus("请填写关卡名称和玩法说明，并确认后端已连接"); return; }
    try {
      const response = await fetch(`${API_URL}/api/v1/level-submissions`, { method: "POST", headers: { "Content-Type": "application/json", Authorization: `Bearer ${sessionToken}` }, body: JSON.stringify(levelForm) });
      if (!response.ok) throw new Error("submit failed");
      setLevelForm({ name: "", description: "", payload: "" });
      setFormStatus("关卡已提交，等待审核");
    } catch { setFormStatus("提交失败，请稍后重试"); }
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

    const isCorrectReport = activeGroup.id === "night-watch" && quotedId === "clue-01";
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
                <b>9:41</b>
                <span>◒ 5G ▰</span>
              </div>
              <div className="wallpaper-sticker sticker-cloud">☁</div>
              <div className="wallpaper-sticker sticker-star">✦</div>
              <div className="home-clock">
                <p>周五 · 纸片镇</p>
                <strong>09:41</strong>
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
                  <small>纸片镇 · 聊天频道</small>
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
                  <small>{activeGroup.tag} · {isBanned && activeGroup.id === "night-watch" ? "已处置" : "正在聊天"}</small>
                </div>
                <div className="header-actions" aria-hidden="true"><span>⌕</span><span>⋯</span></div>
              </header>

              {activeGroup.id === "night-watch" && !isBanned && (
                <div className="mission-tip">
                  <span>!</span>
                  <p>找出反复索要代理节点并攻击群友的发言，点按引用后发送 <b>/spaw</b>。</p>
                </div>
              )}

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
                  return (
                    <article
                      className={`message-row${isSelf ? " self" : ""}${message.isBot ? " bot-row" : ""}`}
                      key={message.id}
                    >
                      {!isSelf && <span className={`message-avatar ${message.tone}`}>{message.avatar}</span>}
                      <div className="message-content">
                        {!isSelf && <b className="sender-name">{message.sender}</b>}
                        <button
                          className={`bubble ${message.tone}${isSelected ? " quoted" : ""}`}
                          onClick={() => message.reportable && setQuotedId(message.id)}
                          disabled={!message.reportable}
                          aria-label={message.reportable ? "引用这条不当消息" : undefined}
                        >
                          <span>{message.text}</span>
                          <small>{message.time}</small>
                        </button>
                      </div>
                    </article>
                  );
                })}
              </div>

              {activeGroup.id === "level-submit" && (
                <div className="portal-form compact-form">
                  <label>关卡名称<input value={levelForm.name} onChange={(event) => setLevelForm({ ...levelForm, name: event.target.value })} placeholder="例如：深夜求节点" /></label>
                  <label>玩法说明<textarea value={levelForm.description} onChange={(event) => setLevelForm({ ...levelForm, description: event.target.value })} placeholder="玩家要找出哪条消息？如何通关？" /></label>
                  <label>关卡数据（可选）<textarea value={levelForm.payload} onChange={(event) => setLevelForm({ ...levelForm, payload: event.target.value })} placeholder="对话脚本或 JSON 数据" /></label>
                  <button onClick={submitLevel}>提交关卡</button>
                  {formStatus && <p className="form-status">{formStatus}</p>}
                </div>
              )}

              {activeGroup.id !== "level-submit" && <div className="composer-wrap">
                {quotedId && (
                  <div className="quote-bar">
                    <span className="quote-stem" />
                    <div>
                      <b>已引用 · 小孩哥</b>
                      <small>[不当索要] 重复索要节点并攻击群友</small>
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
        <section className="npc-stage" aria-label="NPC展示与申请">
          <header className="npc-header">
            <button onClick={() => { setView("chat"); setMobilePane("groups"); }} aria-label="返回聊天">‹</button>
            <div><p>纸片镇角色中心</p><h2>系统 NPC</h2></div>
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
              <p className="portal-kicker">CREATE A PAPER NPC</p>
              <h3>申请新 NPC</h3>
              <p>提交角色设定，审核通过后会出现在系统NPC列表中。</p>
              <div className="portal-form">
                <label>TG账号<input value={npcLookup} onChange={(event) => { setNpcLookup(event.target.value); setNpcEditable(false); }} placeholder="@username 或 https://t.me/username" /></label>
                <button className="extract-button" onClick={extractNPCData}>提取数据</button>
                <label>名称<input disabled={!npcEditable} value={npcForm.name} onChange={(event) => setNpcForm({ ...npcForm, name: event.target.value })} placeholder="提取后自动填充" /></label>
                <label>TG用户名<input disabled={!npcEditable} value={npcForm.tg_username} onChange={(event) => setNpcForm({ ...npcForm, tg_username: event.target.value })} placeholder="提取后自动填充" /></label>
                <label>备注<textarea disabled={!npcEditable} value={npcForm.description} onChange={(event) => setNpcForm({ ...npcForm, description: event.target.value })} placeholder="有简介则自动填充，可修改" /></label>
                <button disabled={!npcEditable} onClick={submitNPC}>提交NPC申请</button>
                {formStatus && <p className="form-status">{formStatus}</p>}
              </div>
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
