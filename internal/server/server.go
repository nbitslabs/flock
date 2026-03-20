package server

import (
	"context"
	"encoding/json"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strings"

	"github.com/nbitslabs/flock/internal/agent"
	"github.com/nbitslabs/flock/internal/auth"
	"github.com/nbitslabs/flock/internal/db/sqlc"
	"github.com/nbitslabs/flock/internal/opencode"
	webfs "github.com/nbitslabs/flock/web"
)

type Server struct {
	mux              *http.ServeMux
	queries          *sqlc.Queries
	manager          *opencode.Manager
	broker           *SSEBroker
	harness          *agent.Harness
	dataDir          string
	basePath         string
	tmpl             *template.Template
	flockAgentClient *opencode.Client
	authEnabled      bool
	authUsername     string
	authPassHash     string
}

func New(queries *sqlc.Queries, manager *opencode.Manager, broker *SSEBroker, harness *agent.Harness, dataDir, basePath string, flockAgentClient *opencode.Client, authEnabled bool, authUsername, authPassHash string) *Server {
	s := &Server{
		mux:              http.NewServeMux(),
		queries:          queries,
		manager:          manager,
		broker:           broker,
		harness:          harness,
		dataDir:          dataDir,
		basePath:         basePath,
		flockAgentClient: flockAgentClient,
		authEnabled:      authEnabled,
		authUsername:     authUsername,
		authPassHash:     authPassHash,
	}
	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	// Parse templates
	tmplFS, err := fs.Sub(webfs.FS, "templates")
	if err != nil {
		log.Fatal("failed to get templates sub-fs:", err)
	}
	s.tmpl = template.Must(template.ParseFS(tmplFS, "*.html"))

	// Static files
	staticFS, err := fs.Sub(webfs.FS, "static")
	if err != nil {
		log.Fatal("failed to get static sub-fs:", err)
	}
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Auth routes
	s.mux.HandleFunc("GET /login", s.handleLoginPage)
	s.mux.HandleFunc("POST /api/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/logout", s.handleLogout)

	// Web UI
	s.mux.HandleFunc("GET /", s.handleIndex)

	// Instance API
	s.mux.HandleFunc("GET /api/instances", s.handleListInstances)
	s.mux.HandleFunc("POST /api/instances", s.handleCreateInstance)
	s.mux.HandleFunc("GET /api/instances/{id}", s.handleGetInstance)
	s.mux.HandleFunc("DELETE /api/instances/{id}", s.handleDeleteInstance)

	// Session API
	s.mux.HandleFunc("GET /api/instances/{id}/sessions", s.handleListSessions)
	s.mux.HandleFunc("POST /api/instances/{id}/sessions", s.handleCreateSession)
	s.mux.HandleFunc("GET /api/instances/{id}/models", s.handleGetModels)
	s.mux.HandleFunc("GET /api/sessions/{id}", s.handleGetSession)
	s.mux.HandleFunc("DELETE /api/sessions/{id}", s.handleDeleteSession)
	s.mux.HandleFunc("GET /api/sessions/{id}/messages", s.handleGetMessages)
	s.mux.HandleFunc("POST /api/sessions/{id}/messages", s.handleSendMessage)
	s.mux.HandleFunc("GET /api/sessions/{id}/events", s.handleSessionEvents)
	s.mux.HandleFunc("PUT /api/sessions/{id}/model", s.handleSetSessionModel)

	// Agent API
	s.mux.HandleFunc("GET /api/instances/{id}/heartbeat", s.handleGetHeartbeat)
	s.mux.HandleFunc("PUT /api/instances/{id}/heartbeat", s.handlePutHeartbeat)
	s.mux.HandleFunc("GET /api/instances/{id}/tasks", s.handleListTasks)
	s.mux.HandleFunc("GET /api/instances/{id}/memory", s.handleGetInstanceMemory)
	s.mux.HandleFunc("GET /api/global-memory", s.handleGetGlobalMemory)
	s.mux.HandleFunc("PUT /api/global-memory", s.handlePutGlobalMemory)
	s.mux.HandleFunc("GET /api/agent/status", s.handleGetAgentStatus)

	// Question API (proxied to OpenCode)
	s.mux.HandleFunc("POST /api/questions/{requestID}/reply", s.handleReplyToQuestion)

	// Memory API
	s.mux.HandleFunc("GET /api/memory/query", s.handleMemoryQuery)
	s.mux.HandleFunc("POST /api/memory/query", s.handleMemoryQuery)
	s.mux.HandleFunc("GET /api/memory/categories", s.handleListMemoryCategories)
	s.mux.HandleFunc("GET /api/memory/cross-repo", s.handleCrossRepoMemoryQuery)
	s.mux.HandleFunc("POST /api/memory/cross-repo", s.handleCrossRepoMemoryQuery)
	s.mux.HandleFunc("GET /api/memory/groups/{group}", s.handleGroupMemory)
	s.mux.HandleFunc("PUT /api/memory/groups/{group}", s.handleGroupMemory)

	// Cross-repo / Manifest API
	s.mux.HandleFunc("GET /api/instances/{id}/manifest", s.handleGetManifest)
	s.mux.HandleFunc("PUT /api/instances/{id}/manifest", s.handlePutManifest)
	s.mux.HandleFunc("GET /api/manifests", s.handleListManifests)
	s.mux.HandleFunc("GET /api/dependency-graph", s.handleGetDependencyGraph)
	s.mux.HandleFunc("GET /api/pr-sets", s.handleListPRSets)
	s.mux.HandleFunc("GET /api/pr-sets/{id}", s.handleGetPRSet)

	// Dashboard API
	s.mux.HandleFunc("GET /api/dashboard/tasks", s.handleDashboardTasks)
	s.mux.HandleFunc("GET /api/dashboard/worktrees", s.handleDashboardWorktrees)
	s.mux.HandleFunc("GET /api/dashboard/memory-stats", s.handleDashboardMemoryStats)
	s.mux.HandleFunc("GET /api/dashboard/sync", s.handleDashboardSync)

	// Flock Agent API
	s.mux.HandleFunc("GET /api/flock-agent", s.handleGetFlockAgentSession)
	s.mux.HandleFunc("POST /api/flock-agent", s.handleCreateFlockAgentSession)
	s.mux.HandleFunc("PUT /api/flock-agent/rotate", s.handleRotateFlockAgentSession)
	s.mux.HandleFunc("GET /api/flock-agent/messages", s.handleGetFlockAgentMessages)
	s.mux.HandleFunc("POST /api/flock-agent/messages", s.handleSendFlockAgentMessage)
	s.mux.HandleFunc("GET /api/flock-agent/events", s.handleFlockAgentEvents)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	s.tmpl.ExecuteTemplate(w, "index.html", nil)
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if !s.authEnabled {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	s.tmpl.ExecuteTemplate(w, "login.html", nil)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.authEnabled {
		http.Error(w, `{"error":"auth not configured"}`, http.StatusInternalServerError)
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	if req.Username != s.authUsername || !auth.CheckPassword(s.authPassHash, req.Password) {
		http.Error(w, `{"error":"invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	token := auth.GenerateToken()
	_, err := s.queries.CreateAuthSession(r.Context(), sqlc.CreateAuthSessionParams{
		Token:    token,
		Username: req.Username,
	})
	if err != nil {
		log.Printf("failed to create auth session: %v", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "flock_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   604800,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("flock_session")
	if err == nil && cookie.Value != "" {
		s.queries.DeleteAuthSession(r.Context(), cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:   "flock_session",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) isValidSession(ctx context.Context, token string) bool {
	if token == "" {
		return false
	}
	session, err := s.queries.GetAuthSession(ctx, token)
	return err == nil && session.Token == token
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if s.authEnabled {
		path := r.URL.Path
		if !strings.HasPrefix(path, "/static/") && path != "/login" && path != "/api/login" && path != "/api/logout" {
			cookie, err := r.Cookie("flock_session")
			if err != nil || !s.isValidSession(r.Context(), cookie.Value) {
				if strings.HasPrefix(path, "/api/") {
					http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				} else {
					http.Redirect(w, r, "/login", http.StatusFound)
				}
				return
			}
		}
	}

	log.Printf("%s %s", r.Method, r.URL.Path)
	s.mux.ServeHTTP(w, r)
}
