package memory

import "embed"

//go:embed templates/*
var templatesFS embed.FS

func defaultHeartbeat() string {
	data, _ := templatesFS.ReadFile("templates/HEARTBEAT.md")
	return string(data)
}

func defaultMemory() string {
	data, _ := templatesFS.ReadFile("templates/MEMORY.md")
	return string(data)
}
