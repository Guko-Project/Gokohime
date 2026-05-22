package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"github.com/colanns/gokohime/internal/config"
	"github.com/colanns/gokohime/internal/database"
)

type Server struct {
	cfg       *config.Config
	db        *gorm.DB
	secret    []byte
	startedAt time.Time
	mux       *http.ServeMux
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type apiErrorResponse struct {
	Error apiError `json:"error"`
}

func NewServer(cfg *config.Config, db *gorm.DB) (*Server, error) {
	if err := database.MigrateAdminModels(db); err != nil {
		return nil, fmt.Errorf("migrate admin models: %w", err)
	}
	if err := ensureInitialAdmin(db, cfg.Admin); err != nil {
		return nil, err
	}
	secret, err := jwtSecret(cfg.Admin)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.Admin.JWTSecret) == "" {
		log.Warn("[admin] admin.jwt_secret is empty; using ephemeral secret for this process")
	}
	server := &Server{cfg: cfg, db: db, secret: secret, startedAt: time.Now(), mux: http.NewServeMux()}
	server.routes()
	return server, nil
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) ListenAndServe() error {
	addr := strings.TrimSpace(s.cfg.Admin.Listen)
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	log.Infof("[admin] listening on http://%s", addr)
	return http.ListenAndServe(addr, s.Handler())
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/auth/login", s.withCORS(s.handleLogin))
	s.mux.HandleFunc("/api/auth/me", s.withCORS(s.requireAuth(s.handleMe)))
	s.mux.HandleFunc("/api/system/health", s.withCORS(s.requireAuth(s.handleHealth)))
	s.mux.HandleFunc("/api/system/stats", s.withCORS(s.requireAuth(s.handleStats)))
	s.mux.HandleFunc("/api/system/config-summary", s.withCORS(s.requireAuth(s.handleConfigSummary)))
	s.mux.HandleFunc("/api/bot-data/tables", s.withCORS(s.requireAuth(s.handleTables)))
	s.mux.HandleFunc("/api/bot-data/tables/", s.withCORS(s.requireAuth(s.handleTableRows)))
	s.mux.HandleFunc("/api/", s.withCORS(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "API route not found")
	}))
	s.mux.HandleFunc("/", s.handleStatic)
}

func (s *Server) withCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if strings.HasPrefix(origin, "http://localhost:") || strings.HasPrefix(origin, "http://127.0.0.1:") {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := verifyToken(s.secret, bearerToken(r))
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Missing or invalid token")
			return
		}
		var user database.AdminUser
		if err := s.db.Where("username = ? AND disabled = ?", claims.Sub, false).First(&user).Error; err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Admin user is unavailable")
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), userContextKey, &user))
		next(w, r)
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use POST")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Invalid JSON body")
		return
	}
	user, err := authenticate(s.db, strings.TrimSpace(req.Username), req.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid username or password")
		return
	}
	token, err := signToken(s.secret, user.Username, 24*time.Hour)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token_error", "Failed to create token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": publicUser(user)})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": publicUser(userFromContext(r.Context()))})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET")
		return
	}
	sqlDB, err := s.db.DB()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", "Failed to inspect database")
		return
	}
	if err := sqlDB.PingContext(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database_unavailable", "Database ping failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "uptime_sec": int64(time.Since(s.startedAt).Seconds())})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET")
		return
	}
	stats := map[string]int64{}
	for _, table := range botDataTables() {
		var count int64
		if err := s.db.Table(table.Name).Count(&count).Error; err != nil {
			writeError(w, http.StatusInternalServerError, "database_error", "Failed to count "+table.Name)
			return
		}
		stats[table.Name] = count
	}
	writeJSON(w, http.StatusOK, map[string]any{"tables": stats})
}

func (s *Server) handleConfigSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"bot": map[string]any{
			"nickname":       s.cfg.Bot.Nickname,
			"command_prefix": s.cfg.Bot.CommandPrefix,
			"rws_url_set":    strings.TrimSpace(s.cfg.Bot.RWSURL) != "",
		},
		"admin": map[string]any{
			"listen":     s.cfg.Admin.Listen,
			"static_dir": s.cfg.Admin.StaticDir,
		},
		"database": map[string]any{
			"driver": s.cfg.Database.Driver(),
			"dsn":    redactDSN(s.cfg.Database.DSN()),
		},
	})
}

func (s *Server) handleTables(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tables": botDataTables()})
}

func (s *Server) handleTableRows(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET")
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/bot-data/tables/")
	name = strings.Trim(name, "/")
	table, ok := botDataTableByName(name)
	if !ok {
		writeError(w, http.StatusNotFound, "table_not_found", "Unknown bot data table")
		return
	}
	limit := parseBoundedInt(r.URL.Query().Get("limit"), 50, 1, 200)
	offset := parseBoundedInt(r.URL.Query().Get("offset"), 0, 0, 100000)
	var count int64
	if err := s.db.Table(table.Name).Count(&count).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", "Failed to count rows")
		return
	}
	rows := []map[string]any{}
	if err := s.db.Table(table.Name).Order("id DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", "Failed to list rows")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"table": table, "total": count, "limit": limit, "offset": offset, "rows": rows})
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET")
		return
	}
	staticDir := strings.TrimSpace(s.cfg.Admin.StaticDir)
	if staticDir == "" {
		staticDir = "web/dist"
	}
	path := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if path == "." || path == string(filepath.Separator) {
		path = "index.html"
	}
	fullPath := filepath.Join(staticDir, path)
	if !strings.HasPrefix(filepath.Clean(fullPath), filepath.Clean(staticDir)) {
		writeError(w, http.StatusBadRequest, "bad_request", "Invalid static path")
		return
	}
	info, err := os.Stat(fullPath)
	if err == nil && !info.IsDir() {
		serveFile(w, r, fullPath)
		return
	}
	indexPath := filepath.Join(staticDir, "index.html")
	if _, err := os.Stat(indexPath); err == nil {
		serveFile(w, r, indexPath)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Gokohime Admin API is running. Build the web app into " + staticDir + " to serve the UI.\n"))
}

func serveFile(w http.ResponseWriter, r *http.Request, path string) {
	if ext := filepath.Ext(path); ext != "" {
		if contentType := mime.TypeByExtension(ext); contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
	}
	http.ServeFile(w, r, path)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, apiErrorResponse{Error: apiError{Code: code, Message: message}})
}

func publicUser(user *database.AdminUser) map[string]any {
	if user == nil {
		return nil
	}
	return map[string]any{"username": user.Username, "display_name": user.DisplayName, "role": user.Role}
}

func parseBoundedInt(raw string, fallback, minValue, maxValue int) int {
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func redactDSN(dsn string) string {
	if dsn == "" {
		return ""
	}
	if strings.Contains(dsn, "password=") || strings.Contains(dsn, "://") {
		return "<redacted>"
	}
	return dsn
}
