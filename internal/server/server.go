package server

import (
	"html/template"
	"io/fs"
	"log"
	"net/http"

	"github.com/nbitslabs/flock/internal/db/sqlc"
	"github.com/nbitslabs/flock/internal/opencode"
	webfs "github.com/nbitslabs/flock/web"
)

type Server struct {
	mux     *http.ServeMux
	queries *sqlc.Queries
	manager *opencode.Manager
	broker  *SSEBroker
	tmpl    *template.Template
}

func New(queries *sqlc.Queries, manager *opencode.Manager, broker *SSEBroker) *Server {
	s := &Server{
		mux:     http.NewServeMux(),
		queries: queries,
		manager: manager,
		broker:  broker,
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
	s.mux.HandleFunc("POST /api/instances/{id}/restore", s.handleRestoreInstance)

	// Session API
	s.mux.HandleFunc("GET /api/instances/{id}/sessions", s.handleListSessions)
	s.mux.HandleFunc("POST /api/instances/{id}/sessions", s.handleCreateSession)
	s.mux.HandleFunc("GET /api/sessions/{id}", s.handleGetSession)
	s.mux.HandleFunc("GET /api/sessions/{id}/messages", s.handleGetMessages)
	s.mux.HandleFunc("POST /api/sessions/{id}/messages", s.handleSendMessage)
	s.mux.HandleFunc("GET /api/sessions/{id}/events", s.handleSessionEvents)
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
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Logging middleware
	log.Printf("%s %s", r.Method, r.URL.Path)
	s.mux.ServeHTTP(w, r)
}
