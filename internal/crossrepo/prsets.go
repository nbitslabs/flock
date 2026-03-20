package crossrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/nbitslabs/flock/internal/db/sqlc"
)

// PRSetManager handles the creation and lifecycle of coordinated PR sets
// that must be merged across multiple repositories in a specific order.
type PRSetManager struct {
	queries *sqlc.Queries
}

// NewPRSetManager creates a new PR set manager.
func NewPRSetManager(queries *sqlc.Queries) *PRSetManager {
	return &PRSetManager{queries: queries}
}

// PRSetMemberInput contains the fields needed to add a PR to a set.
type PRSetMemberInput struct {
	InstanceID string
	Org        string
	Repo       string
	PrUrl      string
	PrNumber   int64
	MergeOrder int64
}

// MergeStatus describes the current state of a PR set and all its members.
type MergeStatus struct {
	SetID       string
	GroupName   string
	Status      string
	Members     []MemberStatus
	NextToMerge string // ID of the next member that should be merged, empty if none
}

// MemberStatus describes the current state of a single PR within a set.
type MemberStatus struct {
	ID         string
	Org        string
	Repo       string
	PrUrl      string
	PrNumber   int64
	Status     string
	MergeOrder int64
	MergedAt   sql.NullTime
}

// CreatePRSet creates a new PR set for coordinating merges across repositories.
// The deploymentOrder slice specifies the order in which repositories should be
// merged (e.g., ["org/backend", "org/api", "org/frontend"]).
func (m *PRSetManager) CreatePRSet(ctx context.Context, groupName string, deploymentOrder []string) (*sqlc.PrSet, error) {
	var orderStr sql.NullString
	if len(deploymentOrder) > 0 {
		data, err := json.Marshal(deploymentOrder)
		if err != nil {
			return nil, fmt.Errorf("marshal deployment order: %w", err)
		}
		orderStr = sql.NullString{String: string(data), Valid: true}
	}

	set, err := m.queries.CreatePRSet(ctx, sqlc.CreatePRSetParams{
		ID:              uuid.New().String(),
		GroupName:       groupName,
		DeploymentOrder: orderStr,
	})
	if err != nil {
		return nil, fmt.Errorf("create pr set: %w", err)
	}
	return &set, nil
}

// AddMember adds a PR to an existing PR set with a specified merge order.
func (m *PRSetManager) AddMember(ctx context.Context, prSetID string, member PRSetMemberInput) (*sqlc.PrSetMember, error) {
	created, err := m.queries.CreatePRSetMember(ctx, sqlc.CreatePRSetMemberParams{
		ID:         uuid.New().String(),
		PrSetID:    prSetID,
		InstanceID: member.InstanceID,
		Org:        member.Org,
		Repo:       member.Repo,
		PrUrl:      member.PrUrl,
		PrNumber:   member.PrNumber,
		MergeOrder: member.MergeOrder,
	})
	if err != nil {
		return nil, fmt.Errorf("add pr set member %s/%s: %w", member.Org, member.Repo, err)
	}
	return &created, nil
}

// CanMerge checks whether a specific member of a PR set is allowed to merge.
// A member can merge only when all members with a lower merge_order have already
// been merged. Returns (canMerge, reason, error).
func (m *PRSetManager) CanMerge(ctx context.Context, prSetID, memberID string) (bool, string, error) {
	members, err := m.queries.ListPRSetMembers(ctx, prSetID)
	if err != nil {
		return false, "", fmt.Errorf("list pr set members: %w", err)
	}

	// Find the target member.
	var target *sqlc.PrSetMember
	for i := range members {
		if members[i].ID == memberID {
			target = &members[i]
			break
		}
	}
	if target == nil {
		return false, "", fmt.Errorf("member %s not found in pr set %s", memberID, prSetID)
	}

	if target.Status == "merged" {
		return false, "already merged", nil
	}

	// Check that all members with a lower merge order are merged.
	for _, member := range members {
		if member.MergeOrder < target.MergeOrder && member.Status != "merged" {
			return false, fmt.Sprintf("waiting for %s/%s (order %d) to be merged first", member.Org, member.Repo, member.MergeOrder), nil
		}
	}

	return true, "ready to merge", nil
}

// MergeMember marks a PR set member as merged. If all members in the set are
// now merged, the set status is updated to "merged" as well.
func (m *PRSetManager) MergeMember(ctx context.Context, prSetID, memberID string) error {
	// Mark the member as merged (sets status and merged_at timestamp).
	if err := m.queries.UpdatePRSetMemberMerged(ctx, memberID); err != nil {
		return fmt.Errorf("mark member merged: %w", err)
	}

	// Check if all members are now merged.
	members, err := m.queries.ListPRSetMembers(ctx, prSetID)
	if err != nil {
		return fmt.Errorf("list pr set members: %w", err)
	}

	allMerged := true
	for _, member := range members {
		if member.Status != "merged" {
			allMerged = false
			break
		}
	}

	if allMerged {
		if err := m.queries.UpdatePRSetStatus(ctx, sqlc.UpdatePRSetStatusParams{
			Status: "merged",
			ID:     prSetID,
		}); err != nil {
			return fmt.Errorf("update pr set status to merged: %w", err)
		}
	}

	return nil
}

// GetMergeStatus returns the current status of a PR set and all its members,
// including which member should be merged next.
func (m *PRSetManager) GetMergeStatus(ctx context.Context, prSetID string) (*MergeStatus, error) {
	set, err := m.queries.GetPRSet(ctx, prSetID)
	if err != nil {
		return nil, fmt.Errorf("get pr set: %w", err)
	}

	members, err := m.queries.ListPRSetMembers(ctx, prSetID)
	if err != nil {
		return nil, fmt.Errorf("list pr set members: %w", err)
	}

	status := &MergeStatus{
		SetID:     set.ID,
		GroupName: set.GroupName,
		Status:    set.Status,
		Members:   make([]MemberStatus, len(members)),
	}

	// Find the next member to merge: the unmerged member with the lowest merge order.
	var nextOrder int64 = -1
	for i, member := range members {
		status.Members[i] = MemberStatus{
			ID:         member.ID,
			Org:        member.Org,
			Repo:       member.Repo,
			PrUrl:      member.PrUrl,
			PrNumber:   member.PrNumber,
			Status:     member.Status,
			MergeOrder: member.MergeOrder,
			MergedAt:   member.MergedAt,
		}

		if member.Status != "merged" && (nextOrder < 0 || member.MergeOrder < nextOrder) {
			nextOrder = member.MergeOrder
			status.NextToMerge = member.ID
		}
	}

	return status, nil
}

// BuildPRSetDescription creates a markdown description for PR bodies that shows
// linked PRs in the set. The currentRepo parameter (in "org/repo" format) is
// used to highlight the current PR's position in the set.
func BuildPRSetDescription(members []sqlc.PrSetMember, currentRepo string) string {
	if len(members) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Coordinated PR Set\n\n")
	b.WriteString("This PR is part of a coordinated merge across multiple repositories.\n")
	b.WriteString("PRs must be merged in the order listed below.\n\n")

	for _, member := range members {
		repoKey := member.Org + "/" + member.Repo
		prefix := "- [ ]"
		if member.Status == "merged" {
			prefix = "- [x]"
		}

		line := fmt.Sprintf("%s **%s** - [PR #%d](%s)", prefix, repoKey, member.PrNumber, member.PrUrl)
		if repoKey == currentRepo {
			line += " **(this PR)**"
		}
		line += "\n"
		b.WriteString(line)
	}

	return b.String()
}

// ValidateDeploymentOrder checks that the proposed deployment order respects
// the dependency graph. Specifically, if repo A depends on repo B, then B must
// appear before A in the deployment order.
func ValidateDeploymentOrder(graph *DependencyGraph, order []string) error {
	if graph == nil {
		return fmt.Errorf("dependency graph is nil")
	}

	// Build a position map for O(1) lookups.
	position := make(map[string]int, len(order))
	for i, repo := range order {
		position[repo] = i
	}

	// For each repo in the order, verify its dependencies come before it.
	for _, repo := range order {
		deps := graph.DependenciesOf(repo)
		repoPos, repoInOrder := position[repo]
		if !repoInOrder {
			continue
		}

		for _, dep := range deps {
			depPos, depInOrder := position[dep]
			if !depInOrder {
				return fmt.Errorf("dependency %s of %s is not included in the deployment order", dep, repo)
			}
			if depPos >= repoPos {
				return fmt.Errorf("invalid deployment order: %s depends on %s, but %s comes after %s in the order", repo, dep, dep, repo)
			}
		}
	}

	return nil
}
