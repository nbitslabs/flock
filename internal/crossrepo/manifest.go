package crossrepo

import (
	"encoding/json"
	"fmt"
	"sort"
)

// Manifest defines a repository's role within a multi-repo group.
type Manifest struct {
	Group           string            `json:"group"`
	Dependencies    []string          `json:"dependencies,omitempty"`
	Consumers       []string          `json:"consumers,omitempty"`
	Contracts       []ContractDef     `json:"contracts,omitempty"`
	DeploymentOrder int               `json:"deployment_order"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

// ContractDef describes an API contract exported by this repository.
type ContractDef struct {
	Type     string   `json:"type"` // openapi, graphql, proto, typescript
	Path     string   `json:"path"`
	Versions []string `json:"versions,omitempty"`
}

// DependencyGraph represents the dependency relationships between repositories.
type DependencyGraph struct {
	Nodes map[string]*RepoNode
	Edges []Edge
}

// RepoNode represents a repository in the dependency graph.
type RepoNode struct {
	Org             string
	Repo            string
	Group           string
	DeploymentOrder int
	Manifest        *Manifest
}

// Edge represents a dependency relationship.
type Edge struct {
	From string // org/repo
	To   string // org/repo
	Type string // "depends_on" or "consumed_by"
}

// ParseManifest parses a JSON manifest string.
func ParseManifest(data string) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &m, nil
}

// ValidateManifest checks a manifest for structural validity.
func ValidateManifest(m *Manifest) error {
	if m.Group == "" {
		return fmt.Errorf("manifest must specify a group name")
	}
	if m.DeploymentOrder < 0 {
		return fmt.Errorf("deployment_order must be non-negative")
	}
	for _, c := range m.Contracts {
		if c.Type == "" {
			return fmt.Errorf("contract type is required")
		}
		if c.Path == "" {
			return fmt.Errorf("contract path is required")
		}
		validTypes := map[string]bool{"openapi": true, "graphql": true, "proto": true, "typescript": true, "json-schema": true}
		if !validTypes[c.Type] {
			return fmt.Errorf("unknown contract type: %s", c.Type)
		}
	}
	return nil
}

// BuildDependencyGraph creates a dependency graph from a set of manifests.
func BuildDependencyGraph(manifests map[string]*Manifest) (*DependencyGraph, error) {
	graph := &DependencyGraph{
		Nodes: make(map[string]*RepoNode),
	}

	for key, m := range manifests {
		graph.Nodes[key] = &RepoNode{
			Org:             orgFromKey(key),
			Repo:            repoFromKey(key),
			Group:           m.Group,
			DeploymentOrder: m.DeploymentOrder,
			Manifest:        m,
		}
	}

	for key, m := range manifests {
		for _, dep := range m.Dependencies {
			if _, ok := graph.Nodes[dep]; !ok {
				return nil, fmt.Errorf("repository %s depends on %s which is not in the manifest set", key, dep)
			}
			graph.Edges = append(graph.Edges, Edge{From: key, To: dep, Type: "depends_on"})
		}
		for _, consumer := range m.Consumers {
			if _, ok := graph.Nodes[consumer]; !ok {
				return nil, fmt.Errorf("repository %s lists consumer %s which is not in the manifest set", key, consumer)
			}
			graph.Edges = append(graph.Edges, Edge{From: consumer, To: key, Type: "consumed_by"})
		}
	}

	// Check for cycles
	if cycle := graph.DetectCycles(); cycle != nil {
		return nil, fmt.Errorf("circular dependency detected: %v", cycle)
	}

	return graph, nil
}

// DetectCycles returns a cycle path if one exists, nil otherwise.
func (g *DependencyGraph) DetectCycles() []string {
	visited := make(map[string]bool)
	inStack := make(map[string]bool)

	var cycle []string
	var dfs func(node string) bool
	dfs = func(node string) bool {
		visited[node] = true
		inStack[node] = true

		for _, e := range g.Edges {
			if e.From != node {
				continue
			}
			neighbor := e.To
			if !visited[neighbor] {
				if dfs(neighbor) {
					cycle = append([]string{neighbor}, cycle...)
					return true
				}
			} else if inStack[neighbor] {
				cycle = []string{neighbor, node}
				return true
			}
		}

		inStack[node] = false
		return false
	}

	for node := range g.Nodes {
		if !visited[node] {
			if dfs(node) {
				return cycle
			}
		}
	}

	return nil
}

// DeploymentOrder returns repositories sorted by deployment order.
func (g *DependencyGraph) DeploymentOrder() []string {
	nodes := make([]string, 0, len(g.Nodes))
	for key := range g.Nodes {
		nodes = append(nodes, key)
	}
	sort.Slice(nodes, func(i, j int) bool {
		return g.Nodes[nodes[i]].DeploymentOrder < g.Nodes[nodes[j]].DeploymentOrder
	})
	return nodes
}

// DependenciesOf returns the direct dependencies of a repository.
func (g *DependencyGraph) DependenciesOf(key string) []string {
	var deps []string
	for _, e := range g.Edges {
		if e.From == key && e.Type == "depends_on" {
			deps = append(deps, e.To)
		}
	}
	return deps
}

// ConsumersOf returns the repositories that consume a given repository.
func (g *DependencyGraph) ConsumersOf(key string) []string {
	var consumers []string
	for _, e := range g.Edges {
		if e.To == key {
			consumers = append(consumers, e.From)
		}
	}
	return consumers
}

// AffectedRepositories returns all repositories that could be affected
// by changes in the given repository (transitive consumers).
func (g *DependencyGraph) AffectedRepositories(key string) []string {
	affected := make(map[string]bool)
	var walk func(string)
	walk = func(node string) {
		for _, consumer := range g.ConsumersOf(node) {
			if !affected[consumer] {
				affected[consumer] = true
				walk(consumer)
			}
		}
	}
	walk(key)

	result := make([]string, 0, len(affected))
	for k := range affected {
		result = append(result, k)
	}
	sort.Strings(result)
	return result
}

func orgFromKey(key string) string {
	for i, c := range key {
		if c == '/' {
			return key[:i]
		}
	}
	return key
}

func repoFromKey(key string) string {
	for i, c := range key {
		if c == '/' {
			return key[i+1:]
		}
	}
	return key
}
