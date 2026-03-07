package opencode

import (
	"bytes"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/nbitslabs/flock/agents"
)

func SyncAgents() error {
	agentsDir, err := getAgentsDir()
	if err != nil {
		return fmt.Errorf("get agents dir: %w", err)
	}

	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		return fmt.Errorf("create agents dir: %w", err)
	}

	entries, err := fs.ReadDir(agents.FS, ".")
	if err != nil {
		return fmt.Errorf("read embedded agents: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		if err := syncAgent(agentsDir, name); err != nil {
			log.Printf("warning: failed to sync agent %s: %v", name, err)
		}
	}

	log.Printf("synced flock agents to %s", agentsDir)
	return nil
}

func syncAgent(agentsDir, agentName string) error {
	embeddedContent, err := fs.ReadFile(agents.FS, agentName+".md")
	if err != nil {
		return fmt.Errorf("read embedded agent: %w", err)
	}

	targetPath := filepath.Join(agentsDir, agentName+".md")

	existingContent, err := os.ReadFile(targetPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read existing agent: %w", err)
	}

	if err == nil && bytes.Equal(existingContent, embeddedContent) {
		return nil
	}

	if err := os.WriteFile(targetPath, embeddedContent, 0644); err != nil {
		return fmt.Errorf("write agent: %w", err)
	}

	log.Printf("installed/updated agent: %s", agentName)
	return nil
}

func getAgentsDir() (string, error) {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("get home dir: %w", err)
		}
		configHome = filepath.Join(home, ".config")
	}

	return filepath.Join(configHome, "opencode", "agents"), nil
}
