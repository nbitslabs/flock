package agent

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
)

type GitHub struct {
	workingDir string
	repo       string
}

func NewGitHub(workingDir string) *GitHub {
	gh := &GitHub{workingDir: workingDir}
	gh.repo = gh.getRepo()
	return gh
}

func (g *GitHub) ReactToIssue(ctx context.Context, issueNumber int, reaction string) error {
	if g.repo == "" {
		return fmt.Errorf("github: no repo configured")
	}
	cmd := exec.CommandContext(ctx, "gh", "issue", "react", fmt.Sprintf("%d", issueNumber), "--repo", g.repo, "-o", reaction)
	cmd.Dir = g.workingDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("github: failed to react to issue #%d with %s: %v (%s)", issueNumber, reaction, err, string(out))
		return err
	}
	log.Printf("github: reacted to issue #%d with %s", issueNumber, reaction)
	return nil
}

func (g *GitHub) CommentOnIssue(ctx context.Context, issueNumber int, comment string) error {
	if g.repo == "" {
		return fmt.Errorf("github: no repo configured")
	}
	cmd := exec.CommandContext(ctx, "gh", "issue", "comment", fmt.Sprintf("%d", issueNumber), "--repo", g.repo, "-b", comment)
	cmd.Dir = g.workingDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("github: failed to comment on issue #%d: %v (%s)", issueNumber, err, string(out))
		return err
	}
	log.Printf("github: commented on issue #%d", issueNumber)
	return nil
}

func (g *GitHub) getRepo() string {
	cmd := exec.Command("gh", "repo", "view", "--json", "owner,repo", "-q", "{{.owner}}/{{.repo}}")
	cmd.Dir = g.workingDir
	out, err := cmd.Output()
	if err != nil {
		log.Printf("github: failed to get repo: %v", err)
		return ""
	}
	return strings.TrimSpace(string(out))
}
