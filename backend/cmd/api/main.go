package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type app struct {
	db              *pgxpool.Pool
	redis           *redis.Client
	botToken        string
	turnstileSecret string
	requireTelegram bool
	allowed         string
}

type sessionRequest struct {
	TGInitData     string         `json:"tg_init_data"`
	TGUserID       string         `json:"tg_user_id"`
	TGContext      map[string]any `json:"tg_context"`
	Fingerprint    map[string]any `json:"fingerprint"`
	WebRTCIps      []string       `json:"webrtc_ips"`
	TurnstileToken string         `json:"turnstile_token"`
}

type player struct {
	ID            string `json:"id"`
	TGUserID      string `json:"tg_user_id,omitempty"`
	Verified      bool   `json:"tg_verified"`
	SessionToken  string `json:"session_token,omitempty"`
	FingerprintID string `json:"fingerprint_id,omitempty"`
	MiniappID     string `json:"miniapp_id,omitempty"`
}

type sessionRecord struct {
	PlayerID      string `json:"player_id"`
	FingerprintID string `json:"fingerprint_id"`
	MiniappID     string `json:"miniapp_id"`
}

type telegramInitUser struct {
	ID           json.Number `json:"id"`
	FirstName    string      `json:"first_name"`
	LastName     string      `json:"last_name"`
	Username     string      `json:"username"`
	PhotoURL     string      `json:"photo_url"`
	LanguageCode string      `json:"language_code"`
}

type levelMessage struct {
	SendID     int64  `json:"send_id"`
	Text       string `json:"text"`
	Reportable bool   `json:"reportable,omitempty"`
}

type levelSubmissionMessage struct {
	NPCID   int64  `json:"npc_id"`
	Message string `json:"message"`
}

func main() {
	ctx := context.Background()
	databaseURL := env("DATABASE_URL", "postgres://game:game@localhost:5432/zhuaxiaohai?sslmode=disable")
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		panic(err)
	}
	defer pool.Close()

	rdb := redis.NewClient(&redis.Options{Addr: env("REDIS_ADDR", "localhost:6379"), Password: os.Getenv("REDIS_PASSWORD")})
	defer rdb.Close()

	a := &app{db: pool, redis: rdb, botToken: os.Getenv("TELEGRAM_BOT_TOKEN"), turnstileSecret: os.Getenv("TURNSTILE_SECRET_KEY"), requireTelegram: env("REQUIRE_TELEGRAM_AUTH", "true") != "false", allowed: env("CORS_ORIGIN", "http://localhost:3000")}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("POST /api/v1/sessions", a.createSession)
	mux.HandleFunc("POST /api/v1/telegram/session", a.createSession)
	mux.HandleFunc("GET /api/v1/me", a.me)
	mux.HandleFunc("POST /api/v1/telegram/events", a.createTelegramEvent)
	mux.HandleFunc("GET /api/v1/npcs", a.listNPCs)
	mux.HandleFunc("GET /api/v1/levels", a.getLevel)
	mux.HandleFunc("GET /api/v1/level-submissions/meta", a.levelSubmissionMeta)
	mux.HandleFunc("POST /api/v1/telegram/extract-profile", a.extractTelegramProfile)
	mux.HandleFunc("POST /api/v1/npc-applications", a.createNPCApplication)
	mux.HandleFunc("GET /api/v1/achievements", a.listAchievements)
	mux.HandleFunc("POST /api/v1/achievements/unlock", a.unlockAchievement)
	mux.HandleFunc("POST /api/v1/level-submissions", a.createLevelSubmission)
	mux.HandleFunc("POST /api/v1/admin/session", a.createAdminSession)
	mux.HandleFunc("POST /api/v1/admin/overview", a.adminOverview)
	mux.HandleFunc("POST /api/v1/admin/fingerprint-labels", a.adminCreateFingerprintLabel)
	mux.HandleFunc("POST /api/v1/admin/review", a.adminReviewApplication)
	mux.HandleFunc("POST /api/v1/admin/delete", a.adminDeleteItem)

	server := &http.Server{Addr: ":" + env("PORT", "8080"), Handler: a.middleware(mux), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		slog.Info("api listening", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			panic(err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdown)
}

func (a *app) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", a.allowed)
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Device-Fingerprint, X-Miniapp-ID")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		next.ServeHTTP(w, r)
	})
}

func (a *app) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.db.Ping(ctx); err != nil {
		fail(w, 503, "postgres unavailable")
		return
	}
	if err := a.redis.Ping(ctx).Err(); err != nil {
		fail(w, 503, "redis unavailable")
		return
	}
	write(w, 200, map[string]string{"status": "ok"})
}

func (a *app) createSession(w http.ResponseWriter, r *http.Request) {
	var in sessionRequest
	if !decode(w, r, &in) {
		return
	}
	if in.TurnstileToken == "" {
		fail(w, 400, "Turnstile token is required")
		return
	}
	if ok, err := a.verifyTurnstile(r.Context(), in.TurnstileToken, requestIP(r)); err != nil || !ok {
		fail(w, 403, "Cloudflare verification failed")
		return
	}
	fingerprintMeta := relayFingerprintMeta(r, lookupIPMetadata(r.Context(), requestIP(r)), lookupWebRTCMetadata(r.Context(), in.WebRTCIps), in.Fingerprint)
	fp, _ := json.Marshal(fingerprintMeta)
	tgContext, _ := json.Marshal(in.TGContext)
	fingerprintID, _ := fingerprintMeta["id"].(string)
	if fingerprintID == "" {
		fail(w, 400, "fingerprint is required")
		return
	}
	verified := false
	if a.botToken != "" && in.TGInitData != "" {
		verified = verifyTelegram(in.TGInitData, a.botToken)
	}
	if a.requireTelegram && !verified {
		fail(w, 401, "invalid Telegram Mini App signature")
		return
	}
	if verified {
		if verifiedUserID := telegramUserID(in.TGInitData); verifiedUserID != "" {
			in.TGUserID = verifiedUserID
		}
	}
	miniappID := strings.TrimSpace(in.TGUserID)
	if miniappID == "" && a.botToken == "" {
		miniappID = "dev-" + fingerprintID
	}
	if miniappID == "" {
		fail(w, 401, "Mini App identifier is required")
		return
	}
	var out player
	err := a.db.QueryRow(r.Context(), `
		INSERT INTO players (tg_user_id, tg_init_data, tg_verified, tg_context, fingerprint_hash, fingerprint)
		VALUES (NULLIF($1,''), NULLIF($2,''), $3, $4, $5, $6)
		ON CONFLICT (fingerprint_hash) DO UPDATE SET
		  tg_user_id = COALESCE(NULLIF(EXCLUDED.tg_user_id,''), players.tg_user_id),
		  tg_init_data = COALESCE(NULLIF(EXCLUDED.tg_init_data,''), players.tg_init_data),
		  tg_verified = EXCLUDED.tg_verified OR players.tg_verified,
		  tg_context = EXCLUDED.tg_context,
		  fingerprint = EXCLUDED.fingerprint, last_seen_at = now()
		RETURNING id, COALESCE(tg_user_id,''), tg_verified`, in.TGUserID, in.TGInitData, verified, tgContext, fingerprintID, fp).Scan(&out.ID, &out.TGUserID, &out.Verified)
	if err != nil {
		serverError(w, err)
		return
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		serverError(w, err)
		return
	}
	out.SessionToken = base64.RawURLEncoding.EncodeToString(tokenBytes)
	out.FingerprintID = fingerprintID
	out.MiniappID = miniappID
	record, _ := json.Marshal(sessionRecord{PlayerID: out.ID, FingerprintID: fingerprintID, MiniappID: miniappID})
	if err := a.redis.Set(r.Context(), "session:"+out.SessionToken, record, 24*time.Hour).Err(); err != nil {
		serverError(w, err)
		return
	}
	write(w, 201, out)
}

func (a *app) listNPCs(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.playerID(w, r); !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT public_id, name, tg_username, description, avatar_url FROM npcs WHERE is_active ORDER BY sort_order, public_id`)
	if err != nil {
		serverError(w, err)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var name, tgUsername, description, avatar string
		if err := rows.Scan(&id, &name, &tgUsername, &description, &avatar); err != nil {
			serverError(w, err)
			return
		}
		items = append(items, map[string]any{"id": id, "name": name, "tg_username": tgUsername, "description": description, "avatar_url": avatar})
	}
	write(w, 200, map[string]any{"items": items})
}

func (a *app) getLevel(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.playerID(w, r); !ok {
		return
	}
	groupID := strings.TrimSpace(r.URL.Query().Get("group_id"))
	if groupID == "" {
		fail(w, 400, "group_id is required")
		return
	}
	var levelNo int64
	var npcIDs []int32
	var npcPhotosRaw, messagesRaw []byte
	err := a.db.QueryRow(r.Context(), `
		SELECT level_no,npc_ids,npc_photos,messages
		FROM game_levels
		WHERE group_id=$1 AND is_active
		ORDER BY updated_at DESC, level_no DESC
		LIMIT 1`, groupID).Scan(&levelNo, &npcIDs, &npcPhotosRaw, &messagesRaw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			fail(w, 404, "level not found")
			return
		}
		serverError(w, err)
		return
	}
	npcIDsOut := make([]int64, 0, len(npcIDs))
	for _, id := range npcIDs {
		npcIDsOut = append(npcIDsOut, int64(id))
	}
	var npcPhotos map[string]string
	if err := json.Unmarshal(npcPhotosRaw, &npcPhotos); err != nil {
		serverError(w, err)
		return
	}
	var messages []levelMessage
	if err := json.Unmarshal(messagesRaw, &messages); err != nil {
		serverError(w, err)
		return
	}
	write(w, 200, map[string]any{"group_id": groupID, "level_no": levelNo, "npc_id": npcIDsOut, "npc_photo": npcPhotos, "messages": messages})
}

func (a *app) me(w http.ResponseWriter, r *http.Request) {
	pid, ok := a.playerID(w, r)
	if !ok {
		return
	}
	var out player
	if err := a.db.QueryRow(r.Context(), `SELECT id,COALESCE(tg_user_id,''),tg_verified FROM players WHERE id=$1`, pid).Scan(&out.ID, &out.TGUserID, &out.Verified); err != nil {
		serverError(w, err)
		return
	}
	write(w, 200, out)
}

func (a *app) createTelegramEvent(w http.ResponseWriter, r *http.Request) {
	pid, ok := a.playerID(w, r)
	if !ok {
		return
	}
	var in struct {
		Event   string         `json:"event"`
		Payload map[string]any `json:"payload"`
	}
	if !decode(w, r, &in) {
		return
	}
	event := strings.TrimSpace(in.Event)
	if event != "miniapp_opened" {
		fail(w, 400, "unsupported event")
		return
	}
	path := "/"
	if rawPath, ok := in.Payload["path"]; ok {
		path = strings.TrimSpace(fmt.Sprint(rawPath))
	}
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") || len([]rune(path)) > 256 {
		fail(w, 400, "invalid event payload")
		return
	}
	payload, _ := json.Marshal(map[string]string{"path": path})
	_, err := a.db.Exec(r.Context(), `INSERT INTO miniapp_events(player_id,event,payload) VALUES($1,$2,$3)`, pid, event, payload)
	if err != nil {
		serverError(w, err)
		return
	}
	write(w, 202, map[string]bool{"accepted": true})
}

func (a *app) extractTelegramProfile(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.playerID(w, r); !ok {
		return
	}
	var in struct {
		Username string `json:"username"`
	}
	if !decode(w, r, &in) {
		return
	}
	username := strings.TrimSpace(in.Username)
	username = strings.TrimPrefix(username, "https://t.me/")
	username = strings.TrimPrefix(username, "http://t.me/")
	username = "@" + strings.TrimPrefix(username, "@")
	if username == "@" {
		fail(w, 400, "username is required")
		return
	}

	var name, description string
	if err := a.db.QueryRow(r.Context(), `SELECT name,description FROM npcs WHERE lower(tg_username)=lower($1) AND is_active`, username).Scan(&name, &description); err == nil {
		write(w, 200, map[string]any{"name": name, "tg_username": username, "description": description, "source": "system"})
		return
	}
	if a.botToken == "" {
		fail(w, 404, "Telegram profile unavailable")
		return
	}
	requestURL := "https://api.telegram.org/bot" + a.botToken + "/getChat?chat_id=" + url.QueryEscape(username)
	request, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, requestURL, nil)
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		fail(w, 502, "Telegram lookup failed")
		return
	}
	defer response.Body.Close()
	var result struct {
		OK     bool `json:"ok"`
		Result struct {
			FirstName   string `json:"first_name"`
			LastName    string `json:"last_name"`
			Title       string `json:"title"`
			Username    string `json:"username"`
			Bio         string `json:"bio"`
			Description string `json:"description"`
		} `json:"result"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil || !result.OK {
		fail(w, 404, "Telegram profile unavailable")
		return
	}
	name = strings.TrimSpace(result.Result.FirstName + " " + result.Result.LastName)
	if name == "" {
		name = result.Result.Title
	}
	description = result.Result.Bio
	if description == "" {
		description = result.Result.Description
	}
	resolvedUsername := username
	if result.Result.Username != "" {
		resolvedUsername = "@" + result.Result.Username
	}
	write(w, 200, map[string]any{"name": name, "tg_username": resolvedUsername, "description": description, "source": "telegram"})
}

func (a *app) createNPCApplication(w http.ResponseWriter, r *http.Request) {
	pid, ok := a.playerID(w, r)
	if !ok {
		return
	}
	var in struct {
		Name          string         `json:"name"`
		TGUsername    string         `json:"tg_username"`
		Description   string         `json:"description"`
		ExtractedData map[string]any `json:"extracted_data"`
		TGInitData    string         `json:"tg_init_data"`
		FingerprintID string         `json:"fingerprint_id"`
		MiniappID     string         `json:"miniapp_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	if a.botToken == "" || !verifyTelegram(in.TGInitData, a.botToken) {
		fail(w, 403, "不符合申请要求")
		return
	}
	tgUser, ok := telegramUser(in.TGInitData)
	tgID := tgUser.ID.String()
	tgUsername := normalizeTelegramUsername(tgUser.Username)
	avatarURL := strings.TrimSpace(tgUser.PhotoURL)
	if !ok || tgID == "" || tgUsername == "" || avatarURL == "" {
		fail(w, 403, "不符合申请要求")
		return
	}
	if !hmac.Equal([]byte(strings.TrimSpace(in.FingerprintID)), []byte(r.Header.Get("X-Device-Fingerprint"))) || !hmac.Equal([]byte(strings.TrimSpace(in.MiniappID)), []byte(r.Header.Get("X-Miniapp-ID"))) || !hmac.Equal([]byte(strings.TrimSpace(in.MiniappID)), []byte(tgID)) {
		fail(w, 403, "不符合申请要求")
		return
	}
	if reserved, err := a.isReservedNPCUsername(r.Context(), tgUsername); err != nil {
		serverError(w, err)
		return
	} else if reserved {
		fail(w, 403, "不符合申请要求")
		return
	}
	if blocked, err := a.isTGBlacklisted(r.Context(), tgID); err != nil {
		serverError(w, err)
		return
	} else if blocked {
		fail(w, 403, "不符合申请要求")
		return
	}
	if blocked, err := a.isFingerprintBlacklisted(r.Context(), in.FingerprintID); err != nil {
		serverError(w, err)
		return
	} else if blocked {
		fail(w, 403, "不符合申请要求")
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = strings.TrimSpace(tgUser.FirstName + " " + tgUser.LastName)
	}
	if name == "" {
		name = strings.TrimPrefix(tgUsername, "@")
	}
	description := strings.TrimSpace(in.Description)
	extractedData := map[string]any{"verified_tg_id": tgID, "verified_tg_username": tgUsername, "verified_avatar_url": avatarURL, "source": "telegram-miniapp"}
	var id string
	extracted, _ := json.Marshal(extractedData)
	fpHash, fpRaw, err := a.playerFingerprint(r.Context(), pid)
	if err != nil {
		serverError(w, err)
		return
	}
	match, err := a.bestFingerprintLabelMatchRaw(r.Context(), fpRaw)
	if err != nil {
		serverError(w, err)
		return
	}
	err = a.db.QueryRow(r.Context(), `
		WITH updated AS (
		  UPDATE npc_applications
		  SET player_id=$1,
		      name=$2,
		      persona=$3,
		      tg_username=$4,
		      description=$5,
		      extracted_data=$6,
		      fingerprint_hash=$7,
		      fingerprint_payload=$8,
		      match_label=$9,
		      match_score=$10,
		      created_at=now()
		  WHERE status='pending'
		    AND (player_id=$1 OR extracted_data->>'verified_tg_id'=$11)
		  RETURNING id
		), inserted AS (
		  INSERT INTO npc_applications(player_id,name,persona,tg_username,description,extracted_data,status,fingerprint_hash,fingerprint_payload,match_label,match_score)
		  SELECT $1,$2,$3,$4,$5,$6,'pending',$7,$8,$9,$10
		  WHERE NOT EXISTS (SELECT 1 FROM updated)
		  RETURNING id
		)
		SELECT id FROM updated
		UNION ALL
		SELECT id FROM inserted
		LIMIT 1`, pid, name, description, tgUsername, description, extracted, fpHash, fpRaw, match.Label, match.Score, tgID).Scan(&id)
	if err != nil {
		serverError(w, err)
		return
	}
	write(w, 201, map[string]any{"id": id, "status": "pending"})
}

func (a *app) listAchievements(w http.ResponseWriter, r *http.Request) {
	pid, ok := a.playerID(w, r)
	if !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT a.code,a.name,a.description,pa.unlocked_at FROM achievements a LEFT JOIN player_achievements pa ON pa.achievement_id=a.id AND pa.player_id=$1 ORDER BY a.sort_order`, pid)
	if err != nil {
		serverError(w, err)
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var code, name, desc string
		var at sql.NullTime
		if err := rows.Scan(&code, &name, &desc, &at); err != nil {
			serverError(w, err)
			return
		}
		items = append(items, map[string]any{"code": code, "name": name, "description": desc, "unlocked": at.Valid, "unlocked_at": at.Time})
	}
	write(w, 200, map[string]any{"items": items})
}

func (a *app) unlockAchievement(w http.ResponseWriter, r *http.Request) {
	pid, ok := a.playerID(w, r)
	if !ok {
		return
	}
	var in struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &in) {
		return
	}
	_, err := a.db.Exec(r.Context(), `INSERT INTO player_achievements(player_id,achievement_id) SELECT $1,id FROM achievements WHERE code=$2 ON CONFLICT DO NOTHING`, pid, in.Code)
	if err != nil {
		serverError(w, err)
		return
	}
	write(w, 200, map[string]bool{"ok": true})
}

func (a *app) levelSubmissionMeta(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.playerID(w, r); !ok {
		return
	}
	groupID := strings.TrimSpace(r.URL.Query().Get("group_id"))
	if groupID == "" {
		fail(w, 400, "group_id is required")
		return
	}
	var groupPrompt func(string) string
	var fixedNPCID int64
	switch groupID {
	case "night-watch":
		fixedNPCID = 9478
		groupPrompt = func(npcIDs string) string {
			return fmt.Sprintf("请根据这些NPC ID [%s，]生成“抓小孩”关卡数据。必须固定使用 npc_id=9478，9478 就是小孩。小孩的人设是：心智不成熟，满口胡话，实际上想要凑近乎白嫖代理节点。只输出JSON数组，格式必须为[{\"npc_id\":id,\"message\":\"发言内容\"},{\"npc_id\":id,\"message\":\"发言内容\"}]。npc_id必须来自给定列表，message不能为空字符串，至少2条消息，并且至少包含1条 npc_id 为9478的小孩发言。", npcIDs)
		}
	case "station":
		fixedNPCID = 9479
		groupPrompt = func(npcIDs string) string {
			return fmt.Sprintf("请根据这些NPC ID [%s，]生成“胡说哥传奇”关卡数据。必须固定使用 npc_id=9479，9479 就是胡说哥。胡说哥的人设是：满口胡话，假装高手，实际上不懂技术。只输出JSON数组，格式必须为[{\"npc_id\":id,\"message\":\"发言内容\"},{\"npc_id\":id,\"message\":\"发言内容\"}]。npc_id必须来自给定列表，message不能为空字符串，至少2条消息，并且至少包含1条 npc_id 为9479的胡说哥发言。", npcIDs)
		}
	default:
		fail(w, 400, "unsupported group_id")
		return
	}
	rows, err := a.db.Query(r.Context(), `
		SELECT public_id
		FROM (
		  SELECT public_id, 0 AS priority FROM npcs WHERE is_active AND public_id=$1
		  UNION ALL
		  SELECT public_id, 1 AS priority
		  FROM (
		    SELECT public_id FROM npcs WHERE is_active AND public_id<>$1 ORDER BY random() LIMIT 9
		  ) random_npcs
		) picked
		ORDER BY priority, public_id`, fixedNPCID)
	if err != nil {
		serverError(w, err)
		return
	}
	defer rows.Close()
	ids := make([]int64, 0, 10)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			serverError(w, err)
			return
		}
		ids = append(ids, id)
	}
	idParts := make([]string, 0, len(ids))
	for _, id := range ids {
		idParts = append(idParts, fmt.Sprintf("%d", id))
	}
	prompt := groupPrompt(strings.Join(idParts, "，"))
	write(w, 200, map[string]any{"group_id": groupID, "npc_ids": ids, "editor_prompt": prompt})
}

func (a *app) createLevelSubmission(w http.ResponseWriter, r *http.Request) {
	pid, ok := a.playerID(w, r)
	if !ok {
		return
	}
	var in struct {
		GroupID     string `json:"group_id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Payload     string `json:"payload"`
	}
	if !decode(w, r, &in) {
		return
	}
	name := strings.TrimSpace(in.Name)
	description := strings.TrimSpace(in.Description)
	groupID := strings.TrimSpace(in.GroupID)
	switch groupID {
	case "night-watch":
		name = "抓小孩"
		description = "心智不成熟，满口胡话，实际上想要凑近乎白嫖代理节点。"
	case "station":
		name = "胡说哥传奇"
		description = "满口胡话，假装高手，实际上不懂技术。"
	default:
		fail(w, 400, "unsupported group_id")
		return
	}
	payload := strings.TrimSpace(in.Payload)
	if name == "" || description == "" || payload == "" {
		fail(w, 400, "请选择关卡种类并填写关卡数据")
		return
	}
	fingerprintID := strings.TrimSpace(r.Header.Get("X-Device-Fingerprint"))
	if blocked, err := a.isFingerprintBlacklisted(r.Context(), fingerprintID); err != nil {
		serverError(w, err)
		return
	} else if blocked {
		fail(w, 403, "不符合提交要求")
		return
	}
	if blocked, err := a.isIPBlacklisted(r.Context(), requestIP(r)); err != nil {
		serverError(w, err)
		return
	} else if blocked {
		fail(w, 403, "不符合提交要求")
		return
	}
	rateKey := "level-submit:rate:" + pid
	if count, err := a.redis.Incr(r.Context(), rateKey).Result(); err != nil {
		serverError(w, err)
		return
	} else {
		if count == 1 {
			_ = a.redis.Expire(r.Context(), rateKey, time.Minute).Err()
		}
		if count > 3 {
			fail(w, 429, "提交过于频繁")
			return
		}
	}
	payloadHash := sha256.Sum256([]byte(pid + "\n" + payload))
	replayKey := "level-submit:replay:" + hex.EncodeToString(payloadHash[:])
	if ok, err := a.redis.SetNX(r.Context(), replayKey, "1", 30*time.Minute).Result(); err != nil {
		serverError(w, err)
		return
	} else if !ok {
		fail(w, 409, "请勿重复提交")
		return
	}
	messages, err := validateLevelPayload(payload)
	if err != nil {
		fail(w, 400, "关卡数据格式不符合要求")
		return
	}
	normalized, _ := json.Marshal(messages)
	var existing bool
	if err := a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM level_submissions WHERE player_id=$1 AND payload=$2)`, pid, string(normalized)).Scan(&existing); err != nil {
		serverError(w, err)
		return
	}
	if existing {
		fail(w, 409, "请勿重复提交")
		return
	}
	fpHash, fpRaw, err := a.playerFingerprint(r.Context(), pid)
	if err != nil {
		serverError(w, err)
		return
	}
	match, err := a.bestFingerprintLabelMatchRaw(r.Context(), fpRaw)
	if err != nil {
		serverError(w, err)
		return
	}
	var id string
	err = a.db.QueryRow(r.Context(), `
		INSERT INTO level_submissions(player_id,name,description,payload,group_id,fingerprint_hash,fingerprint_payload,match_label,match_score)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id`, pid, name, description, string(normalized), groupID, fpHash, fpRaw, match.Label, match.Score).Scan(&id)
	if err != nil {
		serverError(w, err)
		return
	}
	write(w, 201, map[string]string{"id": id, "status": "pending"})
}

func (a *app) createAdminSession(w http.ResponseWriter, r *http.Request) {
	var in adminAuthRequest
	if !decode(w, r, &in) {
		return
	}
	user, ok := a.adminUser(w, r, in)
	if !ok {
		return
	}
	write(w, 200, map[string]any{"ok": true, "username": strings.ToLower(normalizeTelegramUsername(user.Username))})
}

func (a *app) adminOverview(w http.ResponseWriter, r *http.Request) {
	var in adminAuthRequest
	if !decode(w, r, &in) {
		return
	}
	if _, ok := a.adminUser(w, r, in); !ok {
		return
	}
	counts := map[string]int64{}
	for key, query := range map[string]string{
		"players":           `SELECT count(*) FROM players`,
		"npcs":              `SELECT count(*) FROM npcs`,
		"npc_applications":  `SELECT count(*) FROM npc_applications`,
		"level_submissions": `SELECT count(*) FROM level_submissions`,
		"miniapp_events":    `SELECT count(*) FROM miniapp_events`,
	} {
		var count int64
		if err := a.db.QueryRow(r.Context(), query).Scan(&count); err != nil {
			serverError(w, err)
			return
		}
		counts[key] = count
	}
	npcs, err := a.adminListNPCs(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	npcApplications, err := a.adminListNPCApplications(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	levelSubmissions, err := a.adminListLevelSubmissions(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	fingerprints, err := a.adminListRecentFingerprints(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	fingerprintLabels, err := a.adminListFingerprintLabels(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	write(w, 200, map[string]any{"counts": counts, "npcs": npcs, "npc_applications": npcApplications, "level_submissions": levelSubmissions, "fingerprints": fingerprints, "fingerprint_labels": fingerprintLabels})
}

func (a *app) adminCreateFingerprintLabel(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TGInitData    string `json:"tg_init_data"`
		TGUsername    string `json:"tg_username"`
		FingerprintID string `json:"fingerprint_id"`
		MiniappID     string `json:"miniapp_id"`
		LabelName     string `json:"label_name"`
		TargetID      string `json:"target_fingerprint_id"`
		Field         string `json:"field"`
		Value         string `json:"value"`
	}
	if !decode(w, r, &in) {
		return
	}
	if _, ok := a.adminUser(w, r, adminAuthRequest{TGInitData: in.TGInitData, TGUsername: in.TGUsername, FingerprintID: in.FingerprintID, MiniappID: in.MiniappID}); !ok {
		return
	}
	labelName := strings.TrimSpace(in.LabelName)
	fingerprintID := strings.TrimSpace(in.TargetID)
	field := strings.TrimSpace(in.Field)
	value := strings.TrimSpace(in.Value)
	if labelName == "" || field == "" || (fingerprintID == "" && value == "") {
		fail(w, 400, "label_name, field and target_fingerprint_id or value are required")
		return
	}
	var payloadRaw []byte
	if value != "" {
		payload, err := manualFingerprintPayload(field, value)
		if err != nil {
			fail(w, 400, "invalid value")
			return
		}
		payloadRaw, _ = json.Marshal(payload)
		fingerprintID = manualFingerprintID(labelName, field, value)
	} else {
		if err := a.db.QueryRow(r.Context(), `SELECT fingerprint FROM players WHERE fingerprint_hash=$1 ORDER BY last_seen_at DESC LIMIT 1`, fingerprintID).Scan(&payloadRaw); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				fail(w, 404, "fingerprint not found")
				return
			}
			serverError(w, err)
			return
		}
	}
	rules := []string{field}
	var existingRaw []byte
	if err := a.db.QueryRow(r.Context(), `SELECT rules FROM fingerprint_labels WHERE label_name=$1 AND fingerprint_id=$2`, labelName, fingerprintID).Scan(&existingRaw); err == nil {
		_ = json.Unmarshal(existingRaw, &rules)
		rules = appendUniqueString(rules, field)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		serverError(w, err)
		return
	}
	rulesRaw, _ := json.Marshal(rules)
	_, err := a.db.Exec(r.Context(), `
		INSERT INTO fingerprint_labels(label_name,fingerprint_id,fingerprint_payload,rules)
		VALUES($1,$2,$3,$4)
		ON CONFLICT(label_name,fingerprint_id) DO UPDATE SET
		  fingerprint_payload=EXCLUDED.fingerprint_payload,
		  rules=EXCLUDED.rules,
		  updated_at=now()`, labelName, fingerprintID, payloadRaw, rulesRaw)
	if err != nil {
		serverError(w, err)
		return
	}
	write(w, 200, map[string]any{"ok": true, "label_name": labelName, "fingerprint_id": fingerprintID, "rules": rules})
}

func (a *app) adminReviewApplication(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TGInitData    string `json:"tg_init_data"`
		TGUsername    string `json:"tg_username"`
		FingerprintID string `json:"fingerprint_id"`
		MiniappID     string `json:"miniapp_id"`
		Type          string `json:"type"`
		ID            string `json:"id"`
		Action        string `json:"action"`
		LabelName     string `json:"label_name"`
	}
	if !decode(w, r, &in) {
		return
	}
	if _, ok := a.adminUser(w, r, adminAuthRequest{TGInitData: in.TGInitData, TGUsername: in.TGUsername, FingerprintID: in.FingerprintID, MiniappID: in.MiniappID}); !ok {
		return
	}
	itemType := strings.TrimSpace(in.Type)
	id := strings.TrimSpace(in.ID)
	action := strings.TrimSpace(in.Action)
	if itemType == "" || id == "" || action == "" {
		fail(w, 400, "type, id and action are required")
		return
	}
	switch action {
	case "approve":
		if itemType == "npc" {
			if err := a.approveNPCApplication(r.Context(), id); err != nil {
				serverError(w, err)
				return
			}
		} else if itemType == "level" {
			if err := a.approveLevelSubmission(r.Context(), id); err != nil {
				serverError(w, err)
				return
			}
		} else {
			fail(w, 400, "unsupported type")
			return
		}
	case "ignore":
		if err := a.deleteReviewApplication(r.Context(), itemType, id); err != nil {
			serverError(w, err)
			return
		}
	case "mark":
		labelName := strings.TrimSpace(in.LabelName)
		if labelName == "" {
			fail(w, 400, "label_name is required")
			return
		}
		if err := a.markReviewApplication(r.Context(), itemType, id, labelName); err != nil {
			serverError(w, err)
			return
		}
	default:
		fail(w, 400, "unsupported action")
		return
	}
	write(w, 200, map[string]any{"ok": true})
}

func (a *app) adminDeleteItem(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TGInitData    string `json:"tg_init_data"`
		TGUsername    string `json:"tg_username"`
		FingerprintID string `json:"fingerprint_id"`
		MiniappID     string `json:"miniapp_id"`
		Type          string `json:"type"`
		ID            string `json:"id"`
	}
	if !decode(w, r, &in) {
		return
	}
	if _, ok := a.adminUser(w, r, adminAuthRequest{TGInitData: in.TGInitData, TGUsername: in.TGUsername, FingerprintID: in.FingerprintID, MiniappID: in.MiniappID}); !ok {
		return
	}
	switch strings.TrimSpace(in.Type) {
	case "npc":
		if err := a.deleteNPC(r.Context(), strings.TrimSpace(in.ID)); err != nil {
			serverError(w, err)
			return
		}
	case "level_submission":
		if err := a.deleteApprovedLevelSubmission(r.Context(), strings.TrimSpace(in.ID)); err != nil {
			serverError(w, err)
			return
		}
	default:
		fail(w, 400, "unsupported type")
		return
	}
	write(w, 200, map[string]any{"ok": true})
}

func (a *app) approveNPCApplication(ctx context.Context, id string) error {
	var playerID, name, tgUsername, description string
	var extractedRaw []byte
	err := a.db.QueryRow(ctx, `
		SELECT player_id::text,name,tg_username,description,extracted_data
		FROM npc_applications WHERE id=$1`, id).Scan(&playerID, &name, &tgUsername, &description, &extractedRaw)
	if err != nil {
		return err
	}
	var extracted map[string]any
	_ = json.Unmarshal(extractedRaw, &extracted)
	avatarURL := strings.TrimSpace(fmt.Sprint(extracted["verified_avatar_url"]))
	var existingID string
	err = a.db.QueryRow(ctx, `SELECT id FROM npcs WHERE lower(tg_username)=lower($1) LIMIT 1`, tgUsername).Scan(&existingID)
	if err == nil {
		_, err = a.db.Exec(ctx, `UPDATE npcs SET avatar_url=$1, description=$2, is_active=true WHERE id=$3`, avatarURL, description, existingID)
	} else if errors.Is(err, pgx.ErrNoRows) {
		_, err = a.db.Exec(ctx, `INSERT INTO npcs(name,tg_username,description,avatar_url,rarity,sort_order,is_active) VALUES($1,$2,$3,$4,'普通',100,true)`, uniqueNPCName(ctx, a.db, name, tgUsername), tgUsername, description, avatarURL)
	}
	if err != nil {
		return err
	}
	if _, err := a.db.Exec(ctx, `UPDATE npc_applications SET status='approved' WHERE id=$1`, id); err != nil {
		return err
	}
	_, _ = a.db.Exec(ctx, `INSERT INTO player_achievements(player_id,achievement_id) SELECT $1,id FROM achievements WHERE code='npc-creator' ON CONFLICT DO NOTHING`, playerID)
	return nil
}

func (a *app) approveLevelSubmission(ctx context.Context, id string) error {
	var groupID, name, description, payload string
	err := a.db.QueryRow(ctx, `SELECT group_id,name,description,payload FROM level_submissions WHERE id=$1`, id).Scan(&groupID, &name, &description, &payload)
	if err != nil {
		return err
	}
	if groupID == "" {
		switch name {
		case "抓小孩":
			groupID = "night-watch"
		case "胡说哥传奇":
			groupID = "station"
		default:
			return errors.New("missing group_id")
		}
	}
	messages, err := validateLevelPayload(payload)
	if err != nil {
		return err
	}
	npcIDs64 := make([]int64, 0, len(messages))
	seen := map[int64]bool{}
	levelMessages := make([]levelMessage, 0, len(messages))
	for _, item := range messages {
		if !seen[item.NPCID] {
			seen[item.NPCID] = true
			npcIDs64 = append(npcIDs64, item.NPCID)
		}
		levelMessages = append(levelMessages, levelMessage{SendID: item.NPCID, Text: item.Message, Reportable: groupID == "night-watch" && item.NPCID == 9478})
	}
	npcIDs32 := make([]int32, 0, len(npcIDs64))
	for _, npcID := range npcIDs64 {
		npcIDs32 = append(npcIDs32, int32(npcID))
	}
	photos := map[string]string{}
	if len(npcIDs64) > 0 {
		rows, err := a.db.Query(ctx, `SELECT public_id,avatar_url FROM npcs WHERE public_id = ANY($1)`, npcIDs32)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var publicID int64
			var avatar string
			if err := rows.Scan(&publicID, &avatar); err != nil {
				return err
			}
			photos[fmt.Sprintf("%d", publicID)] = avatar
		}
		if err := rows.Err(); err != nil {
			return err
		}
	}
	for _, npcID := range npcIDs64 {
		key := fmt.Sprintf("%d", npcID)
		if _, ok := photos[key]; !ok {
			photos[key] = ""
		}
	}
	var base int
	switch groupID {
	case "night-watch":
		base = 10001
	case "station":
		base = 30001
	default:
		base = 50001
	}
	var levelNo int
	if err := a.db.QueryRow(ctx, `SELECT COALESCE(MAX(level_no), $2-1)+1 FROM game_levels WHERE group_id=$1`, groupID, base).Scan(&levelNo); err != nil {
		return err
	}
	photosRaw, _ := json.Marshal(photos)
	messagesRaw, _ := json.Marshal(levelMessages)
	_, err = a.db.Exec(ctx, `
		INSERT INTO game_levels(group_id,level_no,npc_ids,npc_photos,messages,is_active)
		VALUES($1,$2,$3,$4,$5,true)
		ON CONFLICT(group_id,level_no) DO UPDATE SET
		  npc_ids=EXCLUDED.npc_ids,
		  npc_photos=EXCLUDED.npc_photos,
		  messages=EXCLUDED.messages,
		  is_active=true,
		  updated_at=now()`, groupID, levelNo, npcIDs32, photosRaw, messagesRaw)
	if err != nil {
		return err
	}
	_, err = a.db.Exec(ctx, `UPDATE level_submissions SET status='approved', approved_level_no=$2 WHERE id=$1`, id, levelNo)
	return err
}

func (a *app) deleteNPC(ctx context.Context, publicID string) error {
	id := strings.TrimSpace(publicID)
	if id == "" {
		return errors.New("missing npc id")
	}
	_, err := a.db.Exec(ctx, `UPDATE npcs SET is_active=false WHERE public_id=$1::bigint`, id)
	return err
}

func (a *app) deleteApprovedLevelSubmission(ctx context.Context, id string) error {
	var groupID, name, payload string
	var approvedLevelNo int
	if err := a.db.QueryRow(ctx, `SELECT group_id,name,payload,approved_level_no FROM level_submissions WHERE id=$1`, id).Scan(&groupID, &name, &payload, &approvedLevelNo); err != nil {
		return err
	}
	if groupID == "" {
		switch name {
		case "抓小孩":
			groupID = "night-watch"
		case "胡说哥传奇":
			groupID = "station"
		}
	}
	if groupID != "" && approvedLevelNo > 0 {
		if _, err := a.db.Exec(ctx, `DELETE FROM game_levels WHERE group_id=$1 AND level_no=$2`, groupID, approvedLevelNo); err != nil {
			return err
		}
	} else if groupID != "" {
		if err := a.deleteLevelByPayload(ctx, groupID, payload); err != nil {
			return err
		}
	}
	_, err := a.db.Exec(ctx, `DELETE FROM level_submissions WHERE id=$1`, id)
	return err
}

func (a *app) deleteLevelByPayload(ctx context.Context, groupID, payload string) error {
	messages, err := validateLevelPayload(payload)
	if err != nil {
		return err
	}
	levelMessages := make([]levelMessage, 0, len(messages))
	for index, item := range messages {
		levelMessages = append(levelMessages, levelMessage{SendID: item.NPCID, Text: item.Message, Reportable: groupID == "night-watch" && index == len(messages)-1})
	}
	messagesRaw, _ := json.Marshal(levelMessages)
	_, err = a.db.Exec(ctx, `DELETE FROM game_levels WHERE group_id=$1 AND messages=$2::jsonb`, groupID, string(messagesRaw))
	return err
}

func (a *app) deleteReviewApplication(ctx context.Context, itemType, id string) error {
	switch itemType {
	case "npc":
		_, err := a.db.Exec(ctx, `DELETE FROM npc_applications WHERE id=$1`, id)
		return err
	case "level":
		_, err := a.db.Exec(ctx, `DELETE FROM level_submissions WHERE id=$1`, id)
		return err
	default:
		return errors.New("unsupported type")
	}
}

func (a *app) markReviewApplication(ctx context.Context, itemType, id, labelName string) error {
	var fingerprintID string
	var payloadRaw []byte
	switch itemType {
	case "npc":
		if err := a.db.QueryRow(ctx, `SELECT fingerprint_hash,fingerprint_payload FROM npc_applications WHERE id=$1`, id).Scan(&fingerprintID, &payloadRaw); err != nil {
			return err
		}
	case "level":
		if err := a.db.QueryRow(ctx, `SELECT fingerprint_hash,fingerprint_payload FROM level_submissions WHERE id=$1`, id).Scan(&fingerprintID, &payloadRaw); err != nil {
			return err
		}
	default:
		return errors.New("unsupported type")
	}
	rulesRaw, _ := json.Marshal(allFingerprintRules())
	if _, err := a.db.Exec(ctx, `
		INSERT INTO fingerprint_labels(label_name,fingerprint_id,fingerprint_payload,rules)
		VALUES($1,$2,$3,$4)
		ON CONFLICT(label_name,fingerprint_id) DO UPDATE SET
		  fingerprint_payload=EXCLUDED.fingerprint_payload,
		  rules=EXCLUDED.rules,
		  updated_at=now()`, labelName, fingerprintID, payloadRaw, rulesRaw); err != nil {
		return err
	}
	return a.deleteReviewApplication(ctx, itemType, id)
}

type adminAuthRequest struct {
	TGInitData    string `json:"tg_init_data"`
	TGUsername    string `json:"tg_username"`
	FingerprintID string `json:"fingerprint_id"`
	MiniappID     string `json:"miniapp_id"`
}

func (a *app) adminUser(w http.ResponseWriter, r *http.Request, in adminAuthRequest) (telegramInitUser, bool) {
	if _, ok := a.playerID(w, r); !ok {
		return telegramInitUser{}, false
	}
	if a.botToken == "" || !verifyTelegram(in.TGInitData, a.botToken) {
		fail(w, 403, "forbidden")
		return telegramInitUser{}, false
	}
	tgUser, ok := telegramUser(in.TGInitData)
	tgID := tgUser.ID.String()
	verifiedUsername := strings.ToLower(normalizeTelegramUsername(tgUser.Username))
	submittedUsername := strings.ToLower(normalizeTelegramUsername(in.TGUsername))
	if !ok || verifiedUsername != "@anlianxiaoliu" || submittedUsername != "@anlianxiaoliu" || tgID == "" {
		fail(w, 403, "forbidden")
		return telegramInitUser{}, false
	}
	if !hmac.Equal([]byte(strings.TrimSpace(in.FingerprintID)), []byte(r.Header.Get("X-Device-Fingerprint"))) || !hmac.Equal([]byte(strings.TrimSpace(in.MiniappID)), []byte(r.Header.Get("X-Miniapp-ID"))) || !hmac.Equal([]byte(strings.TrimSpace(in.MiniappID)), []byte(tgID)) {
		fail(w, 403, "forbidden")
		return telegramInitUser{}, false
	}
	return tgUser, true
}

func (a *app) adminListNPCs(ctx context.Context) ([]map[string]any, error) {
	rows, err := a.db.Query(ctx, `SELECT public_id,name,tg_username,description,avatar_url,is_active,created_at FROM npcs WHERE is_active ORDER BY public_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var publicID int64
		var name, username, description, avatarURL string
		var active bool
		var createdAt time.Time
		if err := rows.Scan(&publicID, &name, &username, &description, &avatarURL, &active, &createdAt); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{"id": publicID, "name": name, "tg_username": username, "description": description, "avatar_url": avatarURL, "is_active": active, "created_at": createdAt})
	}
	return items, rows.Err()
}

func (a *app) adminListNPCApplications(ctx context.Context) ([]map[string]any, error) {
	rows, err := a.db.Query(ctx, `SELECT id,name,tg_username,description,status,match_label,match_score,created_at FROM npc_applications ORDER BY created_at DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, name, username, description, status, matchLabel string
		var matchScore float64
		var createdAt time.Time
		if err := rows.Scan(&id, &name, &username, &description, &status, &matchLabel, &matchScore, &createdAt); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{"id": id, "name": name, "tg_username": username, "description": description, "status": status, "match_label": matchLabel, "match_score": matchScore, "created_at": createdAt})
	}
	return items, rows.Err()
}

func (a *app) adminListLevelSubmissions(ctx context.Context) ([]map[string]any, error) {
	rows, err := a.db.Query(ctx, `SELECT id,name,description,payload,status,match_label,match_score,approved_level_no,created_at FROM level_submissions ORDER BY created_at DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, name, description, payload, status, matchLabel string
		var matchScore float64
		var approvedLevelNo int
		var createdAt time.Time
		if err := rows.Scan(&id, &name, &description, &payload, &status, &matchLabel, &matchScore, &approvedLevelNo, &createdAt); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{"id": id, "name": name, "description": description, "payload": payload, "status": status, "match_label": matchLabel, "match_score": matchScore, "approved_level_no": approvedLevelNo, "created_at": createdAt})
	}
	return items, rows.Err()
}

func (a *app) adminListRecentFingerprints(ctx context.Context) ([]map[string]any, error) {
	rows, err := a.db.Query(ctx, `SELECT COALESCE(tg_user_id,''),fingerprint_hash,fingerprint,last_seen_at FROM players ORDER BY last_seen_at DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var tgUserID, fingerprintID string
		var payloadRaw []byte
		var lastSeenAt time.Time
		if err := rows.Scan(&tgUserID, &fingerprintID, &payloadRaw, &lastSeenAt); err != nil {
			return nil, err
		}
		var payload map[string]any
		_ = json.Unmarshal(payloadRaw, &payload)
		items = append(items, map[string]any{"tg_user_id": tgUserID, "fingerprint_id": fingerprintID, "fingerprint": payload, "last_seen_at": lastSeenAt})
	}
	return items, rows.Err()
}

func (a *app) adminListFingerprintLabels(ctx context.Context) ([]map[string]any, error) {
	rows, err := a.db.Query(ctx, `SELECT id,label_name,fingerprint_id,fingerprint_payload,rules,created_at,updated_at FROM fingerprint_labels ORDER BY updated_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, labelName, fingerprintID string
		var payloadRaw, rulesRaw []byte
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &labelName, &fingerprintID, &payloadRaw, &rulesRaw, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		var payload map[string]any
		var rules []string
		_ = json.Unmarshal(payloadRaw, &payload)
		_ = json.Unmarshal(rulesRaw, &rules)
		items = append(items, map[string]any{"id": id, "label_name": labelName, "fingerprint_id": fingerprintID, "fingerprint": payload, "rules": rules, "created_at": createdAt, "updated_at": updatedAt})
	}
	return items, rows.Err()
}

func verifyTelegram(raw, token string) bool {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return false
	}
	provided := values.Get("hash")
	if provided == "" {
		return false
	}
	authDate, err := strconv.ParseInt(values.Get("auth_date"), 10, 64)
	if err != nil || authDate <= 0 {
		return false
	}
	now := time.Now()
	issuedAt := time.Unix(authDate, 0)
	if issuedAt.After(now.Add(5*time.Minute)) || now.Sub(issuedAt) > 24*time.Hour {
		return false
	}
	values.Del("hash")
	parts := make([]string, 0, len(values))
	for key, vals := range values {
		if len(vals) > 0 {
			parts = append(parts, key+"="+vals[0])
		}
	}
	for i := 0; i < len(parts); i++ {
		for j := i + 1; j < len(parts); j++ {
			if parts[j] < parts[i] {
				parts[i], parts[j] = parts[j], parts[i]
			}
		}
	}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(token))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(strings.Join(parts, "\n")))
	return hmac.Equal([]byte(strings.ToLower(provided)), []byte(hex.EncodeToString(mac.Sum(nil))))
}

func telegramUserID(raw string) string {
	values, err := url.ParseQuery(raw)
	if err != nil || values.Get("user") == "" {
		return ""
	}
	var user struct {
		ID json.Number `json:"id"`
	}
	if err := json.Unmarshal([]byte(values.Get("user")), &user); err != nil {
		return ""
	}
	return user.ID.String()
}

func telegramUser(raw string) (telegramInitUser, bool) {
	values, err := url.ParseQuery(raw)
	if err != nil || values.Get("user") == "" {
		return telegramInitUser{}, false
	}
	var user telegramInitUser
	if err := json.Unmarshal([]byte(values.Get("user")), &user); err != nil {
		return telegramInitUser{}, false
	}
	return user, user.ID.String() != ""
}

func normalizeTelegramUsername(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "@")
	if value == "" {
		return ""
	}
	return "@" + value
}

func validateLevelPayload(payload string) ([]levelSubmissionMessage, error) {
	var raw []levelSubmissionMessage
	if err := json.Unmarshal([]byte(payload), &raw); err != nil {
		return nil, err
	}
	if len(raw) < 2 || len(raw) > 100 {
		return nil, errors.New("invalid message count")
	}
	out := make([]levelSubmissionMessage, 0, len(raw))
	for _, item := range raw {
		message := strings.TrimSpace(item.Message)
		if item.NPCID < 0 || message == "" || len([]rune(message)) > 500 {
			return nil, errors.New("invalid message")
		}
		out = append(out, levelSubmissionMessage{NPCID: item.NPCID, Message: message})
	}
	return out, nil
}

func relayFingerprintMeta(r *http.Request, publicIPInfo map[string]string, webrtcIPInfos []map[string]string, fingerprint map[string]any) map[string]any {
	publicInfo := relayIPInfo(publicIPInfo)
	webrtcInfos := make([]map[string]string, 0, len(webrtcIPInfos))
	for _, item := range webrtcIPInfos {
		webrtcInfos = append(webrtcInfos, relayIPInfo(item))
	}
	details := relayFingerprintDetails(fingerprint, relaySystemOS(r))
	payload := stableValue(map[string]any{"publicIpInfo": publicInfo, "webrtcIpInfos": webrtcInfos, "details": details})
	raw, _ := jsonNoHTMLEscape(payload)
	sum := sha256.Sum256(raw)
	return map[string]any{"id": hex.EncodeToString(sum[:])[:24], "publicIpInfo": publicInfo, "webrtcIpInfos": webrtcInfos, "details": details}
}

func relayFingerprintDetails(fingerprint map[string]any, systemOS string) map[string]any {
	osName := trimAny(fingerprint["os"])
	if osName == "" {
		osName = systemOS
	}
	return map[string]any{
		"os":      osName,
		"cpu":     objectValue(fingerprint["cpu"]),
		"screen":  objectValue(fingerprint["screen"]),
		"fonts":   stringArrayValue(fingerprint["fonts"]),
		"canvas":  trimAny(fingerprint["canvas"]),
		"webgl":   objectValue(fingerprint["webgl"]),
		"audio":   trimAny(fingerprint["audio"]),
		"browser": objectValue(fingerprint["browser"]),
	}
}

func relayIPInfo(value map[string]string) map[string]string {
	if value == nil {
		return map[string]string{"ip": "", "asn": "", "organization": ""}
	}
	return map[string]string{"ip": strings.TrimSpace(value["ip"]), "asn": strings.TrimSpace(value["asn"]), "organization": strings.TrimSpace(value["organization"])}
}

func manualFingerprintID(labelName, field, value string) string {
	sum := sha256.Sum256([]byte("manual-fingerprint-label|" + labelName + "|" + field + "|" + value))
	return hex.EncodeToString(sum[:])[:24]
}

func manualFingerprintPayload(field, value string) (map[string]any, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New("empty value")
	}
	payload := map[string]any{
		"publicIpInfo":  map[string]string{"ip": "", "asn": "", "organization": ""},
		"webrtcIpInfos": []map[string]string{},
		"details": map[string]any{
			"os":     "",
			"cpu":    map[string]any{},
			"screen": map[string]any{},
			"fonts":  []string{},
			"canvas": "",
			"webgl":  map[string]any{},
			"audio":  "",
		},
	}
	publicInfo := payload["publicIpInfo"].(map[string]string)
	webrtcInfo := map[string]string{"ip": "", "asn": "", "organization": ""}
	details := payload["details"].(map[string]any)
	switch field {
	case "ip":
		publicInfo["ip"] = value
	case "asn":
		publicInfo["asn"] = value
	case "isp":
		publicInfo["organization"] = value
	case "webrtc_ip":
		webrtcInfo["ip"] = value
		payload["webrtcIpInfos"] = []map[string]string{webrtcInfo}
	case "webrtc_asn":
		webrtcInfo["asn"] = value
		payload["webrtcIpInfos"] = []map[string]string{webrtcInfo}
	case "webrtc_isp":
		webrtcInfo["organization"] = value
		payload["webrtcIpInfos"] = []map[string]string{webrtcInfo}
	case "canvas":
		details["canvas"] = value
	case "webgl":
		details["webgl"] = map[string]any{"hash": value}
	case "audio":
		details["audio"] = value
	case "system":
		details["os"] = value
	case "cpu":
		var parsed map[string]any
		if err := json.Unmarshal([]byte(value), &parsed); err != nil {
			return nil, err
		}
		details["cpu"] = parsed
	case "screen":
		var parsed map[string]any
		if err := json.Unmarshal([]byte(value), &parsed); err != nil {
			return nil, err
		}
		details["screen"] = parsed
	case "fonts":
		parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '，' || r == '\n' })
		fonts := make([]string, 0, len(parts))
		for _, item := range parts {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				fonts = append(fonts, trimmed)
			}
		}
		details["fonts"] = fonts
	default:
		return nil, errors.New("unsupported field")
	}
	return payload, nil
}

func objectValue(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if raw, ok := value.(map[string]any); ok {
		return raw
	}
	return map[string]any{}
}

func stringArrayValue(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		if typed, ok := value.([]string); ok {
			out := make([]string, 0, len(typed))
			for _, item := range typed {
				if trimmed := strings.TrimSpace(item); trimmed != "" {
					out = append(out, trimmed)
				}
			}
			return out
		}
		return []string{}
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if trimmed := trimAny(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func trimAny(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func stableValue(value any) any {
	switch item := value.(type) {
	case []any:
		out := make([]any, 0, len(item))
		for _, entry := range item {
			out = append(out, stableValue(entry))
		}
		return out
	case []map[string]string:
		out := make([]any, 0, len(item))
		for _, entry := range item {
			out = append(out, stableValue(entry))
		}
		return out
	case []string:
		out := make([]any, 0, len(item))
		for _, entry := range item {
			out = append(out, entry)
		}
		return out
	case map[string]string:
		out := make(map[string]any, len(item))
		for key, entry := range item {
			out[key] = entry
		}
		return stableValue(out)
	case map[string]any:
		out := make(map[string]any, len(item))
		for key, entry := range item {
			out[key] = stableValue(entry)
		}
		return out
	default:
		return item
	}
}

func relaySystemOS(r *http.Request) string {
	ua := strings.ToLower(r.UserAgent())
	switch {
	case strings.Contains(ua, "android"):
		return "Android"
	case strings.Contains(ua, "iphone") || strings.Contains(ua, "ipad") || strings.Contains(ua, "ipod"):
		return "iOS"
	case strings.Contains(ua, "windows"):
		return "Windows"
	case strings.Contains(ua, "mac os x") || strings.Contains(ua, "macintosh"):
		return "macOS"
	case strings.Contains(ua, "linux"):
		return "Linux"
	default:
		return "未知"
	}
}

func jsonNoHTMLEscape(value any) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func reservedNPCUsernameFallback(value string) bool {
	switch strings.ToLower(normalizeTelegramUsername(value)) {
	case "@xiaohai", "@thisisabot":
		return true
	default:
		return false
	}
}

func (a *app) isReservedNPCUsername(ctx context.Context, value string) (bool, error) {
	username := strings.ToLower(normalizeTelegramUsername(value))
	if reservedNPCUsernameFallback(username) {
		return true, nil
	}
	var reserved bool
	err := a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reserved_tg_usernames WHERE lower(tg_username)=lower($1))`, username).Scan(&reserved)
	return reserved, err
}

func (a *app) isTGBlacklisted(ctx context.Context, tgID string) (bool, error) {
	var blocked bool
	err := a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tg_blacklist WHERE tg_user_id=$1)`, tgID).Scan(&blocked)
	return blocked, err
}

func (a *app) isFingerprintBlacklisted(ctx context.Context, fingerprintID string) (bool, error) {
	var blocked bool
	err := a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM fingerprint_blacklist WHERE fingerprint_id=$1)`, strings.TrimSpace(fingerprintID)).Scan(&blocked)
	return blocked, err
}

func (a *app) isIPBlacklisted(ctx context.Context, ip string) (bool, error) {
	var blocked bool
	err := a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ip_blacklist WHERE ip=$1)`, strings.TrimSpace(ip)).Scan(&blocked)
	return blocked, err
}

type fingerprintLabelMatch struct {
	Label string
	Score float64
}

func allFingerprintRules() []string {
	return []string{"ip", "asn", "isp", "webrtc_ip", "webrtc_asn", "webrtc_isp", "canvas", "webgl", "audio", "system", "cpu", "screen", "fonts"}
}

func (a *app) playerFingerprint(ctx context.Context, playerID string) (string, []byte, error) {
	var fingerprintID string
	var payloadRaw []byte
	err := a.db.QueryRow(ctx, `SELECT fingerprint_hash,fingerprint FROM players WHERE id=$1`, playerID).Scan(&fingerprintID, &payloadRaw)
	return fingerprintID, payloadRaw, err
}

func (a *app) isFingerprintLabelMatched(ctx context.Context, playerID string) (bool, error) {
	_, payloadRaw, err := a.playerFingerprint(ctx, playerID)
	if err != nil {
		return false, err
	}
	match, err := a.bestFingerprintLabelMatchRaw(ctx, payloadRaw)
	if err != nil {
		return false, err
	}
	return match.Score >= fingerprintMatchThreshold(), nil
}

func (a *app) bestFingerprintLabelMatchRaw(ctx context.Context, payloadRaw []byte) (fingerprintLabelMatch, error) {
	var current map[string]any
	if err := json.Unmarshal(payloadRaw, &current); err != nil {
		return fingerprintLabelMatch{}, err
	}
	rows, err := a.db.Query(ctx, `SELECT label_name,rules,fingerprint_payload FROM fingerprint_labels`)
	if err != nil {
		return fingerprintLabelMatch{}, err
	}
	defer rows.Close()
	best := fingerprintLabelMatch{}
	for rows.Next() {
		var labelName string
		var rulesRaw, targetRaw []byte
		if err := rows.Scan(&labelName, &rulesRaw, &targetRaw); err != nil {
			return fingerprintLabelMatch{}, err
		}
		var rules []string
		var target map[string]any
		_ = json.Unmarshal(rulesRaw, &rules)
		_ = json.Unmarshal(targetRaw, &target)
		score := fingerprintSimilarity(current, target, rules)
		if score > best.Score {
			best = fingerprintLabelMatch{Label: labelName, Score: score}
		}
	}
	if err := rows.Err(); err != nil {
		return fingerprintLabelMatch{}, err
	}
	if best.Score < fingerprintMatchThreshold() {
		best.Label = ""
		best.Score = 0
	}
	return best, nil
}

func fingerprintMatchThreshold() float64 {
	raw := strings.TrimSpace(os.Getenv("FINGERPRINT_MATCH_THRESHOLD"))
	if raw == "" {
		return 0.6
	}
	var value float64
	if _, err := fmt.Sscanf(raw, "%f", &value); err != nil || value <= 0 || value > 1 {
		return 0.6
	}
	return value
}

func fingerprintSimilarity(current, target map[string]any, rules []string) float64 {
	if len(rules) == 0 {
		return 0
	}
	total, matched := 0, 0
	for _, rule := range rules {
		rule = strings.TrimSpace(rule)
		if rule == "" {
			continue
		}
		total++
		if fingerprintRuleMatch(current, target, rule) {
			matched++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(matched) / float64(total)
}

func fingerprintRuleMatch(current, target map[string]any, rule string) bool {
	switch rule {
	case "ip":
		return stringFeature(current, "publicIpInfo.ip") != "" && stringFeature(current, "publicIpInfo.ip") == stringFeature(target, "publicIpInfo.ip")
	case "asn":
		return stringFeature(current, "publicIpInfo.asn") != "" && stringFeature(current, "publicIpInfo.asn") == stringFeature(target, "publicIpInfo.asn")
	case "isp":
		return stringFeature(current, "publicIpInfo.organization") != "" && stringFeature(current, "publicIpInfo.organization") == stringFeature(target, "publicIpInfo.organization")
	case "webrtc_ip":
		return intersects(stringSliceFeature(current, "webrtcIpInfos.ip"), stringSliceFeature(target, "webrtcIpInfos.ip"))
	case "webrtc_asn":
		return intersects(stringSliceFeature(current, "webrtcIpInfos.asn"), stringSliceFeature(target, "webrtcIpInfos.asn"))
	case "webrtc_isp":
		return intersects(stringSliceFeature(current, "webrtcIpInfos.organization"), stringSliceFeature(target, "webrtcIpInfos.organization"))
	case "canvas":
		return stringFeature(current, "details.canvas") != "" && stringFeature(current, "details.canvas") == stringFeature(target, "details.canvas")
	case "webgl":
		currentHash := stringFeature(current, "details.webgl.hash")
		targetHash := stringFeature(target, "details.webgl.hash")
		if currentHash != "" && targetHash != "" {
			return currentHash == targetHash
		}
		return jsonFeature(current, "details.webgl") != "" && jsonFeature(current, "details.webgl") == jsonFeature(target, "details.webgl")
	case "audio":
		return stringFeature(current, "details.audio") != "" && stringFeature(current, "details.audio") == stringFeature(target, "details.audio")
	case "system":
		return stringFeature(current, "details.os") != "" && stringFeature(current, "details.os") == stringFeature(target, "details.os")
	case "cpu":
		return jsonFeature(current, "details.cpu") != "" && jsonFeature(current, "details.cpu") == jsonFeature(target, "details.cpu")
	case "screen":
		return jsonFeature(current, "details.screen") != "" && jsonFeature(current, "details.screen") == jsonFeature(target, "details.screen")
	case "fonts":
		return intersects(stringSliceFeature(current, "details.fonts"), stringSliceFeature(target, "details.fonts"))
	default:
		return false
	}
}

func stringFeature(root map[string]any, path string) string {
	value := nestedValue(root, strings.Split(path, "."))
	return strings.TrimSpace(fmt.Sprint(value))
}

func jsonFeature(root map[string]any, path string) string {
	value := stableValue(nestedValue(root, strings.Split(path, ".")))
	if value == nil {
		return ""
	}
	raw, _ := jsonNoHTMLEscape(value)
	return string(raw)
}

func stringSliceFeature(root map[string]any, path string) []string {
	parts := strings.Split(path, ".")
	if len(parts) < 2 {
		return nil
	}
	parent := nestedValue(root, parts[:len(parts)-1])
	key := parts[len(parts)-1]
	out := []string{}
	switch value := parent.(type) {
	case []any:
		for _, item := range value {
			if row, ok := item.(map[string]any); ok {
				if text := strings.TrimSpace(fmt.Sprint(row[key])); text != "" {
					out = append(out, text)
				}
			}
		}
	case []string:
		for _, item := range value {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				out = append(out, trimmed)
			}
		}
	case []map[string]string:
		for _, item := range value {
			if text := strings.TrimSpace(item[key]); text != "" {
				out = append(out, text)
			}
		}
	}
	return out
}

func nestedValue(root any, parts []string) any {
	current := root
	for _, part := range parts {
		row, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = row[part]
	}
	return current
}

func intersects(a, b []string) bool {
	seen := map[string]bool{}
	for _, item := range a {
		item = strings.TrimSpace(item)
		if item != "" {
			seen[item] = true
		}
	}
	for _, item := range b {
		if seen[strings.TrimSpace(item)] {
			return true
		}
	}
	return false
}

func appendUniqueString(items []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return items
	}
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}

func uniqueNPCName(ctx context.Context, db *pgxpool.Pool, name, tgUsername string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "新NPC"
	}
	var exists bool
	if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM npcs WHERE name=$1)`, name).Scan(&exists); err != nil || !exists {
		return name
	}
	suffix := strings.TrimPrefix(normalizeTelegramUsername(tgUsername), "@")
	if suffix == "" {
		suffix = fmt.Sprintf("%d", time.Now().Unix())
	}
	candidate := name + "-" + suffix
	if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM npcs WHERE name=$1)`, candidate).Scan(&exists); err != nil || !exists {
		return candidate
	}
	return fmt.Sprintf("%s-%d", name, time.Now().Unix())
}

func (a *app) verifyTurnstile(ctx context.Context, token, remoteIP string) (bool, error) {
	if a.turnstileSecret == "" {
		return false, errors.New("TURNSTILE_SECRET_KEY is not configured")
	}
	form := url.Values{"secret": {a.turnstileSecret}, "response": {token}}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://challenges.cloudflare.com/turnstile/v0/siteverify", strings.NewReader(form.Encode()))
	if err != nil {
		return false, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := (&http.Client{Timeout: 8 * time.Second}).Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	var result struct {
		Success bool `json:"success"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return false, err
	}
	return result.Success, nil
}

func requestIP(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); value != "" {
		return value
	}
	if value := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); value != "" {
		return value
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func lookupWebRTCMetadata(ctx context.Context, values []string) []map[string]string {
	items := make([]map[string]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] || !isPublicIP(value) {
			continue
		}
		seen[value] = true
		items = append(items, lookupIPMetadata(ctx, value))
	}
	return items
}

func isPublicIP(value string) bool {
	ip := net.ParseIP(value)
	return ip != nil && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified()
}

func lookupIPMetadata(ctx context.Context, value string) map[string]string {
	empty := map[string]string{"ip": "", "asn": "", "organization": ""}
	if !isPublicIP(value) {
		return empty
	}
	ip := net.ParseIP(value)
	host := ""
	if ipv4 := ip.To4(); ipv4 != nil {
		host = fmt.Sprintf("%d.%d.%d.%d.origin.asn.cymru.com", ipv4[3], ipv4[2], ipv4[1], ipv4[0])
	} else {
		chars := strings.Split(hex.EncodeToString(ip.To16()), "")
		for i, j := 0, len(chars)-1; i < j; i, j = i+1, j-1 {
			chars[i], chars[j] = chars[j], chars[i]
		}
		host = strings.Join(chars, ".") + ".origin6.asn.cymru.com"
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	records, err := net.DefaultResolver.LookupTXT(lookupCtx, host)
	if err != nil || len(records) == 0 {
		return map[string]string{"ip": value, "asn": "", "organization": ""}
	}
	parts := strings.Split(records[0], "|")
	asn := ""
	if len(parts) > 0 {
		asn = strings.TrimSpace(parts[0])
	}
	if asn == "" {
		return map[string]string{"ip": value, "asn": "", "organization": ""}
	}
	asnRecords, err := net.DefaultResolver.LookupTXT(lookupCtx, "AS"+asn+".asn.cymru.com")
	organization, normalizedASN := "", asn
	if err == nil && len(asnRecords) > 0 {
		info := strings.Split(asnRecords[0], "|")
		if len(info) > 0 && strings.TrimSpace(info[0]) != "" {
			normalizedASN = strings.TrimSpace(info[0])
		}
		if len(info) > 4 {
			organization = strings.TrimSpace(info[4])
		}
	}
	return map[string]string{"ip": value, "asn": normalizedASN, "organization": organization}
}

func (a *app) playerID(w http.ResponseWriter, r *http.Request) (string, bool) {
	v := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if v == "" {
		fail(w, 401, "Bearer session token is required")
		return "", false
	}
	raw, err := a.redis.Get(r.Context(), "session:"+v).Result()
	if err != nil {
		fail(w, 401, "session expired")
		return "", false
	}
	var record sessionRecord
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		fail(w, 401, "invalid session")
		return "", false
	}
	if r.Header.Get("X-Device-Fingerprint") == "" || r.Header.Get("X-Miniapp-ID") == "" {
		fail(w, 401, "fingerprint and Mini App identifier are required")
		return "", false
	}
	if !hmac.Equal([]byte(record.FingerprintID), []byte(r.Header.Get("X-Device-Fingerprint"))) || !hmac.Equal([]byte(record.MiniappID), []byte(r.Header.Get("X-Miniapp-ID"))) {
		fail(w, 403, "session binding mismatch")
		return "", false
	}
	return record.PlayerID, true
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		fail(w, 400, "invalid JSON")
		return false
	}
	return true
}
func write(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, msg string) {
	write(w, status, map[string]string{"error": msg})
}
func serverError(w http.ResponseWriter, err error) {
	slog.Error("request failed", "error", err)
	fail(w, 500, "internal server error")
}
func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
