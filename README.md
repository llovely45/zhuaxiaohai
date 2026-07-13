# 抓小孩 Telegram Mini App

公开仓库：https://github.com/llovely45/zhuaxiaohai

Cloudflare Pages：https://zhuaxiaohai.pages.dev

项目采用前后端分离部署：

```text
frontend/            React + Vite纯静态前端，部署到Cloudflare Pages
backend/             Go API、数据库迁移、OpenAPI和Dockerfile
docker-compose.yml   VPS上的Go + PostgreSQL + Redis
```

## Cloudflare Pages部署

在Cloudflare Pages连接本仓库，并填写：

```text
Root directory: frontend
Build command: npm run build
Build output directory: dist
```

添加构建环境变量：

```text
VITE_API_URL=https://zxhapi.942040.xyz
VITE_TURNSTILE_SITE_KEY=Cloudflare Turnstile站点密钥
```

将其替换为VPS后端的HTTPS域名，然后重新部署。Pages提供的HTTPS地址可以直接填写到 BotFather 的 Mini App URL。

当前线上前端地址：

```text
https://zhuaxiaohai.pages.dev
```

也可以手动部署：

```bash
cd frontend
npm ci
VITE_API_URL=https://zxhapi.942040.xyz VITE_TURNSTILE_SITE_KEY=0x4AAAAAADqk0sh7SZ59i2py npm run build
npx wrangler pages deploy dist --project-name zhuaxiaohai
```

## VPS使用源码部署

```bash
git clone https://github.com/llovely45/zhuaxiaohai.git
cd zhuaxiaohai
cp .env.example .env
nano .env
docker compose up -d --build
```

`.env`中必须设置：

```env
POSTGRES_DB=zhuaxiaohai
POSTGRES_USER=game
POSTGRES_PASSWORD=请替换成长随机密码
REDIS_PASSWORD=请替换成另一个长随机密码
TELEGRAM_BOT_TOKEN=从BotFather获取的Token
REQUIRE_TELEGRAM_AUTH=true
TURNSTILE_SECRET_KEY=Cloudflare Turnstile密钥
FINGERPRINT_MATCH_THRESHOLD=0.6
CORS_ORIGIN=https://zhuaxiaohai.pages.dev
API_BIND=127.0.0.1
API_PORT=30001
```

后端默认只监听 `127.0.0.1:30001`，容器内部仍使用 `8080`。使用Nginx或Caddy反向代理到 `https://zxhapi.942040.xyz`，不要把PostgreSQL或Redis端口暴露到公网。

`FINGERPRINT_MATCH_THRESHOLD` 是管理后台“指纹识别”标签的命中阈值，默认 `0.6`。NPC申请命中后直接返回“不符合申请要求”；关卡提交命中后不写入审核表，但仍返回提交成功。

Caddy示例：

```caddyfile
zxhapi.942040.xyz {
    reverse_proxy 127.0.0.1:30001
}
```

## VPS直接拉取GHCR镜像

公开镜像：

```text
ghcr.io/llovely45/zhuaxiaohai-api:latest
```

无需在VPS本地编译Go镜像：

```bash
git clone https://github.com/llovely45/zhuaxiaohai.git
cd zhuaxiaohai
cp .env.example .env
nano .env
docker compose pull api
docker compose up -d --no-build
```

更新版本：

```bash
git pull
docker compose pull api
docker compose up -d --no-build
```

查看状态和日志：

```bash
docker compose ps
docker compose logs -f api
curl http://127.0.0.1:30001/healthz
```

数据库迁移会在API启动前自动执行，PostgreSQL和Redis数据保存在Docker volumes中。不要使用 `docker compose down -v`，除非确定要删除全部数据。

## Telegram Mini App接口

前端已接入 Telegram Web App SDK，包括 `ready()`、`expand()`、安全区、主题、平台、启动参数和 `initData`。后端通过 `TELEGRAM_BOT_TOKEN` 验证签名，以验证后的TG用户ID为准，并签发Redis会话令牌。

首屏先完成Cloudflare Turnstile验证，并使用与 `llovely45/tg-bot-relay` 相同的指纹算法生成24位SHA-256指纹：后端清洗 `publicIpInfo`、`webrtcIpInfos` 与浏览器 `details`，递归按 key 排序后对 JSON 做 SHA-256 并截取前 24 位。会话同时绑定设备指纹与TG Mini App识别码，后续业务请求必须携带：

```text
Authorization: Bearer <session_token>
X-Device-Fingerprint: <fingerprint_id>
X-Miniapp-ID: <verified_tg_user_id>
```

主要接口：

- `POST /api/v1/telegram/session`
- `GET /api/v1/me`
- `POST /api/v1/telegram/events`
- `POST /api/v1/telegram/extract-profile`
- `GET /api/v1/npcs`
- `GET /api/v1/levels?group_id=night-watch`：打开群聊时下发关卡脚本，返回 `level_no`、`npc_id`、`npc_photo` 和 `messages`。前端按 0-1 秒随机间隔逐条弹出消息。
- `POST /api/v1/npc-applications`：提交时必须携带 `tg_init_data`、`fingerprint_id`、`miniapp_id`；后端使用 `TELEGRAM_BOT_TOKEN` 重新验签，只允许有 TG username 和头像、且不在黑名单中的用户写入系统 NPC。同 username 已存在时会使用申请时从 Telegram 凭证解析出的头像 URL 和备注替换旧数据。
- `GET /api/v1/achievements`
- `GET /api/v1/level-submissions/meta?group_id=night-watch`：提交关卡页按关卡种类获取随机最多 10 个系统 NPC ID，并把 NPC ID 写入可复制给 AI 的关卡 JSON 生成提示词。
- `POST /api/v1/level-submissions`：提交关卡。前端只传 `group_id` 和提交审核用的中间格式 `[{"npc_id":9478,"message":"..."}]`，后端会做 session token 绑定校验、防重放、短时间限频、指纹黑名单和 IP 黑名单检查。审核通过后再转成 `game_levels` 的正式关卡格式。

完整定义见 [`backend/openapi.yaml`](backend/openapi.yaml)。

NPC 申请黑名单表：

```sql
INSERT INTO tg_blacklist(tg_user_id, reason) VALUES ('123456789', 'manual');
INSERT INTO fingerprint_blacklist(fingerprint_id, reason) VALUES ('abcdef123456abcdef123456', 'manual');
INSERT INTO ip_blacklist(ip, reason) VALUES ('203.0.113.10', 'manual');
INSERT INTO reserved_tg_usernames(tg_username, reason) VALUES ('@example', 'reserved');
```

`@xiaohai` 和 `@thisisabot` 是系统保留用户名，不能通过 NPC 申请覆盖。

正式关卡数据保存在 `game_levels` 表，游戏只读取这个格式：

```json
{
  "level_no": 10001,
  "npc_id": [1, 9478],
  "npc_photo": { "1": "tg_photo_url_1", "9478": "tg_photo_url_2" },
  "messages": [
    { "send_id": 1, "text": "大家好" },
    { "send_id": 9478, "text": "有没有腾讯云节点", "reportable": true }
  ]
}
```

提交关卡数据暂存在 `level_submissions.payload`，使用审核中间格式：

```json
[
  { "npc_id": 1, "message": "大家好" },
  { "npc_id": 9478, "message": "有没有腾讯云节点" }
]
```

## 本地开发与检查

前端：

```bash
cd frontend
npm ci
npm run dev
```

检查构建：

```bash
cd frontend && npm run build
cd ../backend && go test ./...
docker compose config
```
