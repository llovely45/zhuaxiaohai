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
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type app struct {
	db       *pgxpool.Pool
	redis    *redis.Client
	botToken string
	allowed  string
}

type sessionRequest struct {
	TGInitData  string         `json:"tg_init_data"`
	TGUserID    string         `json:"tg_user_id"`
	TGContext   map[string]any `json:"tg_context"`
	Fingerprint map[string]any `json:"fingerprint"`
}

type player struct {
	ID           string `json:"id"`
	TGUserID     string `json:"tg_user_id,omitempty"`
	Verified     bool   `json:"tg_verified"`
	SessionToken string `json:"session_token,omitempty"`
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

	a := &app{db: pool, redis: rdb, botToken: os.Getenv("TELEGRAM_BOT_TOKEN"), allowed: env("CORS_ORIGIN", "http://localhost:3000")}
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
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
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
	fp, _ := json.Marshal(in.Fingerprint)
	tgContext, _ := json.Marshal(in.TGContext)
	hash := sha256.Sum256(fp)
	verified := false
	if a.botToken != "" && in.TGInitData != "" {
		verified = verifyTelegram(in.TGInitData, a.botToken)
	}
	if in.TGInitData != "" && a.botToken != "" && !verified {
		fail(w, 401, "invalid Telegram Mini App signature")
		return
	}
	if verified {
		if verifiedUserID := telegramUserID(in.TGInitData); verifiedUserID != "" {
			in.TGUserID = verifiedUserID
		}
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
		RETURNING id, COALESCE(tg_user_id,''), tg_verified`, in.TGUserID, in.TGInitData, verified, tgContext, hex.EncodeToString(hash[:]), fp).Scan(&out.ID, &out.TGUserID, &out.Verified)
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
	if err := a.redis.Set(r.Context(), "session:"+out.SessionToken, out.ID, 24*time.Hour).Err(); err != nil {
		serverError(w, err)
		return
	}
	write(w, 201, out)
}

func (a *app) listNPCs(w http.ResponseWriter, r *http.Request) {
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
		ExtractedData map[string]any `json:"extracted_data"`
	}
	if !decode(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.TGUsername) == "" {
		fail(w, 400, "name and tg_username are required")
		return
	}
	var id string
	extracted, _ := json.Marshal(in.ExtractedData)
	err := a.db.QueryRow(r.Context(), `INSERT INTO npc_applications(player_id,name,persona,tg_username,description,extracted_data) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, pid, in.Name, in.Description, in.TGUsername, in.Description, extracted).Scan(&id)
	if err != nil {
		serverError(w, err)
		return
	}
	_, _ = a.db.Exec(r.Context(), `INSERT INTO player_achievements(player_id,achievement_id) SELECT $1,id FROM achievements WHERE code='npc-creator' ON CONFLICT DO NOTHING`, pid)
	write(w, 201, map[string]string{"id": id, "status": "pending"})
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

func (a *app) playerID(w http.ResponseWriter, r *http.Request) (string, bool) {
	v := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if v == "" {
		fail(w, 401, "Bearer session token is required")
		return "", false
	}
	playerID, err := a.redis.Get(r.Context(), "session:"+v).Result()
	if err != nil {
		fail(w, 401, "session expired")
		return "", false
	}
	return playerID, true
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
