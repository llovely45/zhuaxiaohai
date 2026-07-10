"use client";

import { useEffect, useMemo, useState } from "react";

type View = "home" | "handoff" | "chat";
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

const groups: ChatGroup[] = [
  {
    id: "night-watch",
    name: "纸片闲聊群",
    tag: "43 人",
    color: "pink",
    preview: "小板凳：又来问节点了…",
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
        sender: "小板凳",
        avatar: "板",
        tone: "pink",
        text: "又有人有能用的代理节点吗？没有就别装懂。",
        time: "09:43",
        reportable: true,
      },
      {
        id: "night-02",
        sender: "泡泡糖",
        avatar: "晚",
        tone: "mint",
        text: "他已经连着问好几次了，还开始怼人。请引用原消息交给机器人处理。",
        time: "09:43",
      },
    ],
  },
  {
    id: "station",
    name: "北桥便利站",
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
    name: "纸片放映社",
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
    name: "群规小助手",
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

function formatTimer(seconds: number) {
  const total = Math.max(0, seconds);
  const minutes = Math.floor(total / 60);
  const displaySeconds = Math.floor(total % 60);
  const centiseconds = Math.floor((total * 100) % 100);
  return `${String(minutes).padStart(2, "0")}:${String(displaySeconds).padStart(
    2,
    "0",
  )}.${String(centiseconds).padStart(2, "0")}`;
}

export default function Home() {
  const [view, setView] = useState<View>("home");
  const [activeGroupId, setActiveGroupId] = useState("night-watch");
  const [mobilePane, setMobilePane] = useState<MobilePane>("groups");
  const [quotedId, setQuotedId] = useState<string | null>(null);
  const [draft, setDraft] = useState("");
  const [startedAt, setStartedAt] = useState<number | null>(null);
  const [elapsed, setElapsed] = useState(0);
  const [completed, setCompleted] = useState(false);
  const [isBanned, setIsBanned] = useState(false);
  const [addedMessages, setAddedMessages] = useState<Record<string, ChatMessage[]>>({});

  const activeGroup = useMemo(
    () => groups.find((group) => group.id === activeGroupId) ?? groups[0],
    [activeGroupId],
  );
  const activeMessages = [...activeGroup.messages, ...(addedMessages[activeGroup.id] ?? [])];

  useEffect(() => {
    if (view !== "handoff") return;
    const nextScreen = window.setTimeout(() => {
      setView("chat");
      setMobilePane("groups");
    }, 1350);
    return () => window.clearTimeout(nextScreen);
  }, [view]);

  useEffect(() => {
    if (!startedAt || completed) return;
    const clock = window.setInterval(() => {
      setElapsed((Date.now() - startedAt) / 1000);
    }, 40);
    return () => window.clearInterval(clock);
  }, [completed, startedAt]);

  const addMessage = (groupId: string, message: ChatMessage) => {
    setAddedMessages((current) => ({
      ...current,
      [groupId]: [...(current[groupId] ?? []), message],
    }));
  };

  const startGame = () => {
    if (view !== "home") return;
    setStartedAt(Date.now());
    setElapsed(0);
    setView("handoff");
  };

  const openGroup = (groupId: string) => {
    setActiveGroupId(groupId);
    setQuotedId(null);
    setMobilePane("messages");
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
          setElapsed(startedAt ? (Date.now() - startedAt) / 1000 : elapsed);
          setCompleted(true);
        }, 820);
      } else {
        addMessage(activeGroup.id, {
          id: `hint-${Date.now()}`,
          sender: "群规机器人",
          avatar: "安",
          tone: "bot",
          isBot: true,
          text: "暂未能处理。请在“纸片闲聊群”引用反复索要代理节点并攻击群友的消息后再提交。",
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
    setStartedAt(null);
    setElapsed(0);
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
              <footer>网络热梗改编 · 非官方</footer>
            </blockquote>
            <div className="home-tags" aria-label="游戏特点">
              <span>竖屏对话游戏</span>
              <span>二次元纸片风</span>
            </div>
          </div>

          <figure className="hanhong-cutout" aria-label="二次元韩红纸片人，网络热梗致敬">
            <img src="/hanhong-paper.png" alt="手持麦克风和手机的二次元韩红纸片人" />
            <figcaption>热梗致敬 · 非官方形象</figcaption>
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
          <header className="task-strip">
            <div>
              <span className="task-pin">✦</span>
              <p>当前任务</p>
              <strong>引用捣乱消息后发送 /spaw</strong>
            </div>
            <div className="timer-card" aria-label={`已用时 ${formatTimer(elapsed)}`}>
              <span>用时</span>
              <b>{formatTimer(elapsed)}</b>
            </div>
          </header>

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
                <p className="list-heading">群聊 <span>4</span></p>
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

              <div className="composer-wrap">
                {quotedId && (
                  <div className="quote-bar">
                    <span className="quote-stem" />
                    <div>
                      <b>已引用 · 小板凳</b>
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
              </div>
            </section>
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
            <div className="result-time">
              <span>本轮用时</span>
              <strong>{formatTimer(elapsed)}</strong>
            </div>
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
