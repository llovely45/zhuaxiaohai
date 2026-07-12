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
CORS_ORIGIN=https://zhuaxiaohai.pages.dev
API_BIND=127.0.0.1
```

后端默认只监听 `127.0.0.1:8080`。使用Nginx或Caddy反向代理到 `https://zxhapi.942040.xyz`，不要把PostgreSQL或Redis端口暴露到公网。

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
curl http://127.0.0.1:8080/healthz
```

数据库迁移会在API启动前自动执行，PostgreSQL和Redis数据保存在Docker volumes中。不要使用 `docker compose down -v`，除非确定要删除全部数据。

## Telegram Mini App接口

前端已接入 Telegram Web App SDK，包括 `ready()`、`expand()`、安全区、主题、平台、启动参数和 `initData`。后端通过 `TELEGRAM_BOT_TOKEN` 验证签名，以验证后的TG用户ID为准，并签发Redis会话令牌。

首屏先完成Cloudflare Turnstile验证，并使用与 `llovely45/tg-bot-relay` 相同的OS、CPU、屏幕、字体、Canvas、WebGL、Audio、浏览器和WebRTC信号生成24位SHA-256指纹。会话同时绑定设备指纹与TG Mini App识别码，后续业务请求必须携带：

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
- `POST /api/v1/npc-applications`
- `GET /api/v1/achievements`
- `POST /api/v1/level-submissions`

完整定义见 [`backend/openapi.yaml`](backend/openapi.yaml)。

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
