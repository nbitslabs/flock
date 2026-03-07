package server

import (
	"html/template"
	"io/fs"
	"log"
	"net/http"

	"github.com/nbitslabs/flock/internal/agent"
	"github.com/nbitslabs/flock/internal/db/sqlc"
	"github.com/nbitslabs/flock/internal/opencode"
	webfs "github.com/nbitslabs/flock/web"
)

type Server struct {
	mux                *http.ServeMux
	queries            *sqlc.Queries
	manager            *opencode.Manager
	broker             *SSEBroker
	harness            *agent.Harness
	dataDir            string
	tmpl               *template.Template
	flockAgentClient   *opencode.Client
}

func New(queries *sqlc.Queries, manager *opencode.Manager, broker *SSEBroker, harness *agent.Harness, dataDir string, flockAgentClient *opencode.Client) *Server {
	s := &Server{
		mux:              http.NewServeMux(),
		queries:          queries,
		manager:          manager,
		broker:           broker,
		harness:          harness,
		dataDir:          dataDir,
		flockAgentClient: flockAgentClient,
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
	s.mux.HandleFunc("GET /api/sessions/{id}", s.handleGetSession)
	s.mux.HandleFunc("DELETE /api/sessions/{id}", s.handleDeleteSession)
	s.mux.HandleFunc("GET /api/sessions/{id}/messages", s.handleGetMessages)
	s.mux.HandleFunc("POST /api/sessions/{id}/messages", s.handleSendMessage)
	s.mux.HandleFunc("GET /api/sessions/{id}/events", s.handleSessionEvents)

	// Agent API
	s.mux.HandleFunc("GET /api/instances/{id}/heartbeat", s.handleGetHeartbeat)
	s.mux.HandleFunc("PUT /api/instances/{id}/heartbeat", s.handlePutHeartbeat)
	s.mux.HandleFunc("GET /api/instances/{id}/tasks", s.handleListTasks)
	s.mux.HandleFunc("GET /api/instances/{id}/memory", s.handleGetInstanceMemory)
	s.mux.HandleFunc("GET /api/global-memory", s.handleGetGlobalMemory)
	s.mux.HandleFunc("PUT /api/global-memory", s.handlePutGlobalMemory)
	s.mux.HandleFunc("GET /api/agent/status", s.handleGetAgentStatus)

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

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// CORS middleware
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Logging middleware
	log.Printf("%s %s", r.Method, r.URL.Path)
	s.mux.ServeHTTP(w, r)
}
