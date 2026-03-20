package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nbitslabs/flock/internal/crossrepo"
	"github.com/nbitslabs/flock/internal/memory"
)

type memoryQueryRequest struct {
	Query    string `json:"query"`
	Category string `json:"category"`
	Tag      string `json:"tag"`
	Limit    int    `json:"limit"`
}

type memoryQueryResult struct {
	Path      string            `json:"path"`
	Category  string            `json:"category"`
	Title     string            `json:"title"`
	Snippet   string            `json:"snippet"`
	Fields    map[string]string `json:"fields"`
	Score     float64           `json:"score"`
	UpdatedAt string            `json:"updated_at"`
}

type memoryQueryResponse struct {
	Results    []memoryQueryResult `json:"results"`
	Total      int                 `json:"total"`
	Categories []string            `json:"categories"`
}

// handleMemoryQuery searches memory files with optional query, category, and tag filtering.
func (s *Server) handleMemoryQuery(w http.ResponseWriter, r *http.Request) {
	var req memoryQueryRequest

	// Support both GET params and POST body
	if r.Method == "POST" {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
	} else {
		req.Query = r.URL.Query().Get("q")
		req.Category = r.URL.Query().Get("category")
		req.Tag = r.URL.Query().Get("tag")
	}

	if req.Limit <= 0 || req.Limit > 100 {
		req.Limit = 20
	}

	// Get instance context for org/repo
	instanceID := r.URL.Query().Get("instance_id")
	if instanceID == "" {
		// Try to get from path
		instanceID = r.PathValue("id")
	}

	var org, repo string
	if instanceID != "" {
		inst, err := s.queries.GetInstance(r.Context(), instanceID)
		if err == nil {
			org = inst.Org
			repo = inst.Repo
		}
	}

	// Fall back to scanning all repos
	results := s.searchMemoryFiles(org, repo, req)

	resp := memoryQueryResponse{
		Results:    results,
		Total:      len(results),
		Categories: memory.ListCategories(),
	}

	writeJSON(w, resp)
}

// handleListMemoryCategories returns available memory categories with their schemas.
func (s *Server) handleListMemoryCategories(w http.ResponseWriter, r *http.Request) {
	type categoryInfo struct {
		Name             string   `json:"name"`
		Description      string   `json:"description"`
		RequiredHeadings []string `json:"required_headings"`
		RequiredFields   []string `json:"required_fields"`
		Template         string   `json:"template"`
	}

	var cats []categoryInfo
	for _, cat := range memory.Categories {
		cats = append(cats, categoryInfo{
			Name:             cat.Name,
			Description:      cat.Description,
			RequiredHeadings: cat.RequiredHeadings,
			RequiredFields:   cat.RequiredFields,
			Template:         cat.Template,
		})
	}

	writeJSON(w, cats)
}

// searchMemoryFiles scans memory directories for files matching the query.
func (s *Server) searchMemoryFiles(org, repo string, req memoryQueryRequest) []memoryQueryResult {
	var results []memoryQueryResult
	stateDir := memory.ResolveStateDir(s.dataDir)

	// Determine search roots
	var searchRoots []string
	if org != "" && repo != "" {
		searchRoots = []string{memory.RepoStatePath(s.dataDir, org, repo)}
	} else {
		// Scan all repo state directories
		ghDir := filepath.Join(stateDir, "state", "github.com")
		if orgDirs, err := os.ReadDir(ghDir); err == nil {
			for _, orgDir := range orgDirs {
				if !orgDir.IsDir() {
					continue
				}
				repoDirs, err := os.ReadDir(filepath.Join(ghDir, orgDir.Name()))
				if err != nil {
					continue
				}
				for _, repoDir := range repoDirs {
					if repoDir.IsDir() {
						searchRoots = append(searchRoots, filepath.Join(ghDir, orgDir.Name(), repoDir.Name()))
					}
				}
			}
		}
	}

	queryLower := strings.ToLower(req.Query)

	for _, root := range searchRoots {
		filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".md") {
				return nil
			}
			// Skip non-memory files
			name := info.Name()
			if name == "HEARTBEAT.md" {
				return nil
			}

			content, err := os.ReadFile(path)
			if err != nil {
				return nil
			}

			contentStr := string(content)

			// Parse frontmatter
			fm, _ := memory.ParseFrontmatter(contentStr)

			// Apply category filter
			if req.Category != "" {
				fileCategory := detectCategory(contentStr, fm)
				if fileCategory != req.Category {
					return nil
				}
			}

			// Apply tag filter
			if req.Tag != "" {
				tags := fm["tags"]
				if !strings.Contains(strings.ToLower(tags), strings.ToLower(req.Tag)) {
					return nil
				}
			}

			// Score by relevance
			score := 0.0
			if queryLower != "" {
				contentLower := strings.ToLower(contentStr)
				if strings.Contains(contentLower, queryLower) {
					// Count occurrences for scoring
					score = float64(strings.Count(contentLower, queryLower))
					// Boost for title matches
					title := extractTitle(contentStr)
					if strings.Contains(strings.ToLower(title), queryLower) {
						score += 10
					}
				} else {
					return nil // no match
				}
			} else {
				score = 1 // all files match when no query
			}

			// Boost by recency
			modTime := info.ModTime()
			daysSinceModified := time.Since(modTime).Hours() / 24
			if daysSinceModified < 7 {
				score += 5
			} else if daysSinceModified < 30 {
				score += 2
			}

			result := memoryQueryResult{
				Path:      path,
				Category:  detectCategory(contentStr, fm),
				Title:     extractTitle(contentStr),
				Snippet:   extractSnippet(contentStr, queryLower),
				Fields:    fm,
				Score:     score,
				UpdatedAt: modTime.Format(time.RFC3339),
			}

			results = append(results, result)
			return nil
		})
	}

	// Sort by score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// Apply limit
	if len(results) > req.Limit {
		results = results[:req.Limit]
	}

	return results
}

// detectCategory tries to determine the category of a memory file.
func detectCategory(content string, fm memory.Frontmatter) string {
	if cat, ok := fm["category"]; ok {
		return cat
	}

	// Try to detect from headings
	headings := strings.ToLower(content)
	for name, cat := range memory.Categories {
		matched := 0
		for _, h := range cat.RequiredHeadings {
			if strings.Contains(headings, strings.ToLower("## "+h)) {
				matched++
			}
		}
		if matched == len(cat.RequiredHeadings) {
			return name
		}
	}

	return "uncategorized"
}

// extractTitle extracts the first # heading from markdown.
func extractTitle(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") && !strings.HasPrefix(trimmed, "## ") {
			return strings.TrimPrefix(trimmed, "# ")
		}
	}
	return ""
}

// extractSnippet returns a context snippet around the query match.
func extractSnippet(content, query string) string {
	if query == "" {
		// Return first non-frontmatter paragraph
		lines := strings.Split(content, "\n")
		inFrontmatter := false
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "---" {
				inFrontmatter = !inFrontmatter
				continue
			}
			if inFrontmatter || trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			if len(trimmed) > 200 {
				return trimmed[:200] + "..."
			}
			return trimmed
		}
		return ""
	}

	contentLower := strings.ToLower(content)
	idx := strings.Index(contentLower, query)
	if idx == -1 {
		return ""
	}

	start := idx - 50
	if start < 0 {
		start = 0
	}
	end := idx + len(query) + 100
	if end > len(content) {
		end = len(content)
	}

	snippet := content[start:end]
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(content) {
		snippet = snippet + "..."
	}

	return snippet
}

// --- Cross-repo memory types ---

type crossRepoMemoryResult struct {
	memoryQueryResult
	SourceRepo string  `json:"source_repo"`
	Proximity  float64 `json:"proximity"`
}

type crossRepoMemoryRequest struct {
	Group    string `json:"group"`
	Query    string `json:"query"`
	Category string `json:"category"`
	Tag      string `json:"tag"`
	Limit    int    `json:"limit"`
}

type crossRepoMemoryResponse struct {
	Results    []crossRepoMemoryResult `json:"results"`
	Total      int                     `json:"total"`
	Group      string                  `json:"group"`
	Categories []string                `json:"categories"`
}

// handleCrossRepoMemoryQuery searches memory across all repositories in a group,
// annotating results with source repository and proximity-based ranking.
func (s *Server) handleCrossRepoMemoryQuery(w http.ResponseWriter, r *http.Request) {
	var req crossRepoMemoryRequest

	if r.Method == "POST" {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
	} else {
		req.Group = r.URL.Query().Get("group")
		req.Query = r.URL.Query().Get("q")
		req.Category = r.URL.Query().Get("category")
		req.Tag = r.URL.Query().Get("tag")
	}

	if req.Group == "" {
		http.Error(w, `{"error":"group parameter is required"}`, http.StatusBadRequest)
		return
	}

	if req.Limit <= 0 || req.Limit > 100 {
		req.Limit = 20
	}

	// Find all repos in the group
	manifests, err := s.queries.ListManifestsByGroup(r.Context(), req.Group)
	if err != nil {
		log.Printf("cross-repo memory: list manifests error: %v", err)
		http.Error(w, `{"error":"failed to list group manifests"}`, http.StatusInternalServerError)
		return
	}

	if len(manifests) == 0 {
		writeJSON(w, crossRepoMemoryResponse{
			Results:    []crossRepoMemoryResult{},
			Total:      0,
			Group:      req.Group,
			Categories: memory.ListCategories(),
		})
		return
	}

	// Build dependency graph for proximity scoring
	manifestMap := make(map[string]*crossrepo.Manifest)
	for _, m := range manifests {
		key := fmt.Sprintf("%s/%s", m.Org, m.Repo)
		parsed, err := crossrepo.ParseManifest(m.ManifestJson)
		if err != nil {
			continue
		}
		manifestMap[key] = parsed
	}

	var depGraph *crossrepo.DependencyGraph
	if len(manifestMap) > 1 {
		depGraph, _ = crossrepo.BuildDependencyGraph(manifestMap)
	}

	// Determine source repo for proximity if instance_id is provided
	sourceKey := ""
	if instanceID := r.URL.Query().Get("instance_id"); instanceID != "" {
		inst, err := s.queries.GetInstance(r.Context(), instanceID)
		if err == nil {
			sourceKey = fmt.Sprintf("%s/%s", inst.Org, inst.Repo)
		}
	}

	// Build a set of direct dependencies for the source repo
	directDeps := make(map[string]bool)
	if sourceKey != "" && depGraph != nil {
		for _, dep := range depGraph.DependenciesOf(sourceKey) {
			directDeps[dep] = true
		}
		// Also include direct consumers as close relationships
		for _, consumer := range depGraph.ConsumersOf(sourceKey) {
			directDeps[consumer] = true
		}
	}

	// Search memory across all repos in the group
	memReq := memoryQueryRequest{
		Query:    req.Query,
		Category: req.Category,
		Tag:      req.Tag,
		Limit:    100, // get more results before proximity filtering
	}

	var allResults []crossRepoMemoryResult
	for _, m := range manifests {
		repoResults := s.searchMemoryFiles(m.Org, m.Repo, memReq)
		repoKey := fmt.Sprintf("%s/%s", m.Org, m.Repo)

		for _, result := range repoResults {
			proximity := computeProximity(sourceKey, repoKey, directDeps, depGraph)
			allResults = append(allResults, crossRepoMemoryResult{
				memoryQueryResult: result,
				SourceRepo:        repoKey,
				Proximity:         proximity,
			})
		}
	}

	// Sort by combined relevance (score) + proximity boost
	sort.Slice(allResults, func(i, j int) bool {
		scoreI := allResults[i].Score + allResults[i].Proximity
		scoreJ := allResults[j].Score + allResults[j].Proximity
		return scoreI > scoreJ
	})

	// Apply limit
	if len(allResults) > req.Limit {
		allResults = allResults[:req.Limit]
	}

	writeJSON(w, crossRepoMemoryResponse{
		Results:    allResults,
		Total:      len(allResults),
		Group:      req.Group,
		Categories: memory.ListCategories(),
	})
}

// computeProximity returns a proximity boost score based on the relationship
// between the source repo and the target repo in the dependency graph.
func computeProximity(sourceKey, targetKey string, directDeps map[string]bool, graph *crossrepo.DependencyGraph) float64 {
	if sourceKey == "" {
		return 0
	}
	// Same repo gets highest boost
	if sourceKey == targetKey {
		return 10.0
	}
	// Direct dependency or consumer gets a strong boost
	if directDeps[targetKey] {
		return 5.0
	}
	// Transitive relationship gets a smaller boost
	if graph != nil {
		affected := graph.AffectedRepositories(targetKey)
		for _, a := range affected {
			if a == sourceKey {
				return 2.0
			}
		}
		affected = graph.AffectedRepositories(sourceKey)
		for _, a := range affected {
			if a == targetKey {
				return 2.0
			}
		}
	}
	// Same group but no direct relationship
	return 1.0
}

// handleGroupMemory handles reading and writing group-level memory.
func (s *Server) handleGroupMemory(w http.ResponseWriter, r *http.Request) {
	groupName := r.PathValue("group")
	if groupName == "" {
		http.Error(w, `{"error":"group name is required"}`, http.StatusBadRequest)
		return
	}

	switch r.Method {
	case "GET":
		content, err := memory.ReadGroupMemory(s.dataDir, groupName)
		if err != nil {
			log.Printf("read group memory %s: %v", groupName, err)
			http.Error(w, `{"error":"failed to read group memory"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{
			"group":   groupName,
			"content": content,
		})

	case "PUT":
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB limit
		if err != nil {
			http.Error(w, `{"error":"failed to read request body"}`, http.StatusBadRequest)
			return
		}

		var req struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
			return
		}

		if err := memory.WriteGroupMemory(s.dataDir, groupName, req.Content); err != nil {
			log.Printf("write group memory %s: %v", groupName, err)
			http.Error(w, `{"error":"failed to write group memory"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{
			"group":  groupName,
			"status": "ok",
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
