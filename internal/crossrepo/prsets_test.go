package crossrepo

import (
	"database/sql"
	"testing"
	"time"

	"github.com/nbitslabs/flock/internal/db/sqlc"
)

func TestBuildPRSetDescription(t *testing.T) {
	members := []sqlc.PrSetMember{
		{
			ID:         "m1",
			PrSetID:    "set1",
			InstanceID: "inst1",
			Org:        "acme",
			Repo:       "backend",
			PrUrl:      "https://github.com/acme/backend/pull/10",
			PrNumber:   10,
			Status:     "merged",
			MergeOrder: 0,
			MergedAt:   sql.NullTime{Time: time.Now(), Valid: true},
			CreatedAt:  time.Now(),
		},
		{
			ID:         "m2",
			PrSetID:    "set1",
			InstanceID: "inst2",
			Org:        "acme",
			Repo:       "api",
			PrUrl:      "https://github.com/acme/api/pull/20",
			PrNumber:   20,
			Status:     "pending",
			MergeOrder: 1,
			CreatedAt:  time.Now(),
		},
		{
			ID:         "m3",
			PrSetID:    "set1",
			InstanceID: "inst3",
			Org:        "acme",
			Repo:       "frontend",
			PrUrl:      "https://github.com/acme/frontend/pull/30",
			PrNumber:   30,
			Status:     "pending",
			MergeOrder: 2,
			CreatedAt:  time.Now(),
		},
	}

	desc := BuildPRSetDescription(members, "acme/api")

	// Verify header is present.
	if !contains(desc, "Coordinated PR Set") {
		t.Error("expected header 'Coordinated PR Set'")
	}

	// Verify merged member has checkbox checked.
	if !contains(desc, "- [x] **acme/backend**") {
		t.Error("expected merged member to have checked checkbox")
	}

	// Verify pending members have unchecked checkboxes.
	if !contains(desc, "- [ ] **acme/api**") {
		t.Error("expected pending member to have unchecked checkbox")
	}

	// Verify current repo is highlighted.
	if !contains(desc, "(this PR)") {
		t.Error("expected current repo to be highlighted with '(this PR)'")
	}

	// Verify PR links are present.
	if !contains(desc, "[PR #10](https://github.com/acme/backend/pull/10)") {
		t.Error("expected PR link for backend")
	}
	if !contains(desc, "[PR #20](https://github.com/acme/api/pull/20)") {
		t.Error("expected PR link for api")
	}
}

func TestBuildPRSetDescriptionEmpty(t *testing.T) {
	desc := BuildPRSetDescription(nil, "acme/api")
	if desc != "" {
		t.Errorf("expected empty string for nil members, got %q", desc)
	}
}

func TestValidateDeploymentOrder(t *testing.T) {
	manifests := map[string]*Manifest{
		"org/backend":  {Group: "myapp", DeploymentOrder: 0},
		"org/api":      {Group: "myapp", Dependencies: []string{"org/backend"}, DeploymentOrder: 1},
		"org/frontend": {Group: "myapp", Dependencies: []string{"org/api"}, DeploymentOrder: 2},
	}
	graph, err := BuildDependencyGraph(manifests)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		order   []string
		wantErr bool
	}{
		{
			name:    "valid order matching dependencies",
			order:   []string{"org/backend", "org/api", "org/frontend"},
			wantErr: false,
		},
		{
			name:    "invalid: frontend before api",
			order:   []string{"org/backend", "org/frontend", "org/api"},
			wantErr: true,
		},
		{
			name:    "invalid: api before backend",
			order:   []string{"org/api", "org/backend", "org/frontend"},
			wantErr: true,
		},
		{
			name:    "invalid: completely reversed",
			order:   []string{"org/frontend", "org/api", "org/backend"},
			wantErr: true,
		},
		{
			name:    "missing dependency in order",
			order:   []string{"org/api", "org/frontend"},
			wantErr: true,
		},
		{
			name:    "partial order with only leaf",
			order:   []string{"org/backend"},
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateDeploymentOrder(graph, tc.order)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateDeploymentOrder(%v) error = %v, wantErr %v", tc.order, err, tc.wantErr)
			}
		})
	}
}

func TestValidateDeploymentOrderNilGraph(t *testing.T) {
	err := ValidateDeploymentOrder(nil, []string{"org/a"})
	if err == nil {
		t.Error("expected error for nil graph")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsSubstring(s, substr)
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
