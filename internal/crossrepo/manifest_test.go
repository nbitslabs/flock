package crossrepo

import (
	"testing"
)

func TestParseManifest(t *testing.T) {
	data := `{"group":"mygroup","dependencies":["org/backend"],"deployment_order":2,"contracts":[{"type":"openapi","path":"api/openapi.yaml"}]}`
	m, err := ParseManifest(data)
	if err != nil {
		t.Fatal(err)
	}
	if m.Group != "mygroup" {
		t.Errorf("expected mygroup, got %s", m.Group)
	}
	if len(m.Dependencies) != 1 || m.Dependencies[0] != "org/backend" {
		t.Errorf("unexpected dependencies: %v", m.Dependencies)
	}
	if m.DeploymentOrder != 2 {
		t.Errorf("expected deployment_order 2, got %d", m.DeploymentOrder)
	}
	if len(m.Contracts) != 1 || m.Contracts[0].Type != "openapi" {
		t.Errorf("unexpected contracts: %v", m.Contracts)
	}
}

func TestValidateManifest(t *testing.T) {
	tests := []struct {
		name    string
		m       Manifest
		wantErr bool
	}{
		{"valid", Manifest{Group: "g", DeploymentOrder: 0}, false},
		{"missing group", Manifest{}, true},
		{"negative order", Manifest{Group: "g", DeploymentOrder: -1}, true},
		{"invalid contract type", Manifest{Group: "g", Contracts: []ContractDef{{Type: "bad", Path: "x"}}}, true},
		{"missing contract path", Manifest{Group: "g", Contracts: []ContractDef{{Type: "openapi"}}}, true},
	}
	for _, tc := range tests {
		err := ValidateManifest(&tc.m)
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: err=%v, wantErr=%v", tc.name, err, tc.wantErr)
		}
	}
}

func TestBuildDependencyGraph(t *testing.T) {
	manifests := map[string]*Manifest{
		"org/backend":  {Group: "myapp", DeploymentOrder: 0},
		"org/api":      {Group: "myapp", Dependencies: []string{"org/backend"}, DeploymentOrder: 1},
		"org/frontend": {Group: "myapp", Dependencies: []string{"org/api"}, DeploymentOrder: 2},
	}

	graph, err := BuildDependencyGraph(manifests)
	if err != nil {
		t.Fatal(err)
	}

	if len(graph.Nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(graph.Nodes))
	}

	order := graph.DeploymentOrder()
	if order[0] != "org/backend" || order[2] != "org/frontend" {
		t.Errorf("unexpected deployment order: %v", order)
	}

	deps := graph.DependenciesOf("org/frontend")
	if len(deps) != 1 || deps[0] != "org/api" {
		t.Errorf("unexpected dependencies: %v", deps)
	}

	affected := graph.AffectedRepositories("org/backend")
	if len(affected) != 2 {
		t.Errorf("expected 2 affected repos, got %d: %v", len(affected), affected)
	}
}

func TestDetectCycles(t *testing.T) {
	manifests := map[string]*Manifest{
		"org/a": {Group: "g", Dependencies: []string{"org/b"}},
		"org/b": {Group: "g", Dependencies: []string{"org/a"}},
	}

	_, err := BuildDependencyGraph(manifests)
	if err == nil {
		t.Error("expected cycle detection error")
	}
}

func TestMissingDependency(t *testing.T) {
	manifests := map[string]*Manifest{
		"org/a": {Group: "g", Dependencies: []string{"org/missing"}},
	}

	_, err := BuildDependencyGraph(manifests)
	if err == nil {
		t.Error("expected missing dependency error")
	}
}

func TestOrgRepoFromKey(t *testing.T) {
	if orgFromKey("myorg/myrepo") != "myorg" {
		t.Error("expected myorg")
	}
	if repoFromKey("myorg/myrepo") != "myrepo" {
		t.Error("expected myrepo")
	}
}
