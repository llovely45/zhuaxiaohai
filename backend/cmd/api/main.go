package main

import (
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
	mux.HandleFunc("POST /api/v1/telegram/extract-profile", a.extractTelegramProfile)
	mux.HandleFunc("POST /api/v1/npc-applications", a.createNPCApplication)
	mux.HandleFunc("GET /api/v1/achievements", a.listAchievements)
	mux.HandleFunc("POST /api/v1/achievements/unlock", a.unlockAchievement)
	mux.HandleFunc("POST /api/v1/level-submissions", a.createLevelSubmission)

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
	fingerprintSource := map[string]any{
		"publicIpInfo":  lookupIPMetadata(r.Context(), requestIP(r)),
		"webrtcIpInfos": lookupWebRTCMetadata(r.Context(), in.WebRTCIps),
		"details":       in.Fingerprint,
	}
	fp, _ := json.Marshal(fingerprintSource)
	tgContext, _ := json.Marshal(in.TGContext)
	hash := sha256.Sum256(fp)
	fingerprintID := hex.EncodeToString(hash[:])[:24]
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
	if strings.TrimSpace(in.Event) == "" {
		fail(w, 400, "event is required")
		return
	}
	payload, _ := json.Marshal(in.Payload)
	_, err := a.db.Exec(r.Context(), `INSERT INTO miniapp_events(player_id,event,payload) VALUES($1,$2,$3)`, pid, in.Event, payload)
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
		AvatarURL     string         `json:"avatar_url"`
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
	if in.ExtractedData == nil {
		in.ExtractedData = map[string]any{}
	}
	in.ExtractedData["verified_tg_id"] = tgID
	in.ExtractedData["verified_tg_username"] = tgUsername
	in.ExtractedData["verified_avatar_url"] = avatarURL
	in.ExtractedData["source"] = "telegram-miniapp"
	var id string
	extracted, _ := json.Marshal(in.ExtractedData)
	err := a.db.QueryRow(r.Context(), `INSERT INTO npc_applications(player_id,name,persona,tg_username,description,extracted_data,status) VALUES($1,$2,$3,$4,$5,$6,'approved') RETURNING id`, pid, name, description, tgUsername, description, extracted).Scan(&id)
	if err != nil {
		serverError(w, err)
		return
	}
	var existingID string
	var npcPublicID int64
	var npcName, npcUsername, npcDescription, npcAvatar string
	err = a.db.QueryRow(r.Context(), `SELECT id FROM npcs WHERE lower(tg_username)=lower($1) LIMIT 1`, tgUsername).Scan(&existingID)
	if err == nil {
		err = a.db.QueryRow(r.Context(), `UPDATE npcs SET avatar_url=$1, description=$2, is_active=true WHERE id=$3 RETURNING public_id,name,tg_username,description,avatar_url`, avatarURL, description, existingID).Scan(&npcPublicID, &npcName, &npcUsername, &npcDescription, &npcAvatar)
	} else if errors.Is(err, pgx.ErrNoRows) {
		err = a.db.QueryRow(r.Context(), `INSERT INTO npcs(name,tg_username,description,avatar_url,rarity,sort_order,is_active) VALUES($1,$2,$3,$4,'普通',100,true) RETURNING public_id,name,tg_username,description,avatar_url`, uniqueNPCName(r.Context(), a.db, name, tgUsername), tgUsername, description, avatarURL).Scan(&npcPublicID, &npcName, &npcUsername, &npcDescription, &npcAvatar)
	}
	if err != nil {
		serverError(w, err)
		return
	}
	_, _ = a.db.Exec(r.Context(), `INSERT INTO player_achievements(player_id,achievement_id) SELECT $1,id FROM achievements WHERE code='npc-creator' ON CONFLICT DO NOTHING`, pid)
	write(w, 201, map[string]any{"id": id, "status": "approved", "npc": map[string]any{"id": npcPublicID, "name": npcName, "tg_username": npcUsername, "description": npcDescription, "avatar_url": npcAvatar}})
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

func (a *app) createLevelSubmission(w http.ResponseWriter, r *http.Request) {
	pid, ok := a.playerID(w, r)
	if !ok {
		return
	}
	var in struct{ Name, Description, Payload string }
	if !decode(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Description) == "" {
		fail(w, 400, "name and description are required")
		return
	}
	var id string
	err := a.db.QueryRow(r.Context(), `INSERT INTO level_submissions(player_id,name,description,payload) VALUES($1,$2,$3,$4) RETURNING id`, pid, in.Name, in.Description, in.Payload).Scan(&id)
	if err != nil {
		serverError(w, err)
		return
	}
	write(w, 201, map[string]string{"id": id, "status": "pending"})
}

func verifyTelegram(raw, token string) bool {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return false
	}
	provided := values.Get("hash")
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
