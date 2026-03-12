package ssh

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ensureHostKeyOnce  sync.Once
	ensureHostKeyError error
)

const (
	knownHostsFileName = "known_hosts"
	sshDirName         = "ssh"
	githubHost         = "github.com"
	keyScanTimeout     = 10 * time.Second
)

func EnsureGitHubHostKey() error {
	ensureHostKeyOnce.Do(func() {
		ensureHostKeyError = ensureGitHubHostKey()
	})
	return ensureHostKeyError
}

func ensureGitHubHostKey() error {
	knownHostsPath, err := getKnownHostsPath()
	if err != nil {
		return fmt.Errorf("failed to get known_hosts path: %w", err)
	}

	if _, err := os.Stat(knownHostsPath); err == nil {
		if containsHost(knownHostsPath, githubHost) {
			return nil
		}
	}

	if err := addGitHubHostKey(knownHostsPath); err != nil {
		return fmt.Errorf("failed to add GitHub host key: %w", err)
	}

	return nil
}

func getKnownHostsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	sshDir := filepath.Join(home, ".flock", sshDirName)
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create SSH directory: %w", err)
	}

	return filepath.Join(sshDir, knownHostsFileName), nil
}

func containsHost(knownHostsPath, host string) bool {
	data, err := os.ReadFile(knownHostsPath)
	if err != nil {
		return false
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, host+" ") || strings.HasPrefix(line, host+",") {
			return true
		}
	}
	return false
}

func addGitHubHostKey(knownHostsPath string) error {
	if _, err := exec.LookPath("ssh-keyscan"); err != nil {
		return fmt.Errorf("ssh-keyscan not found: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), keyScanTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ssh-keyscan", "-t", "rsa,dsa,ecdsa,ed25519", githubHost)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("ssh-keyscan failed: %w", err)
	}

	f, err := os.OpenFile(knownHostsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open known_hosts: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(output); err != nil {
		return fmt.Errorf("failed to write known_hosts: %w", err)
	}

	return nil
}
