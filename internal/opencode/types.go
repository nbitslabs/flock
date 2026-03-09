package opencode

import "encoding/json"

// OpenCode API types — matches the actual opencode serve API responses

type SessionTime struct {
	Created int64 `json:"created"`
	Updated int64 `json:"updated"`
}

type Session struct {
	ID        string      `json:"id"`
	Slug      string      `json:"slug,omitempty"`
	Title     string      `json:"title"`
	Directory string      `json:"directory,omitempty"`
	ParentID  string      `json:"parentID,omitempty"`
	Version   string      `json:"version,omitempty"`
	ProjectID string      `json:"projectID,omitempty"`
	Time      SessionTime `json:"time"`
}

type CreateSessionRequest struct{}

type MessageTime struct {
	Created   int64 `json:"created"`
	Completed int64 `json:"completed,omitempty"`
}

type MessageInfo struct {
	ID         string      `json:"id"`
	Role       string      `json:"role"`
	ParentID   string      `json:"parentID,omitempty"`
	ModelID    string      `json:"modelID,omitempty"`
	ProviderID string      `json:"providerID,omitempty"`
	Time       MessageTime `json:"time"`
}

type Message struct {
	Info  MessageInfo       `json:"info"`
	Parts []json.RawMessage `json:"parts"`
}

type Part struct {
	Type    string `json:"type"`
	Content string `json:"content,omitempty"`
	Text    string `json:"text,omitempty"`
}

type SendMessageRequest struct {
	Parts []SendPart `json:"parts"`
}

type SendMessageRequestWithModel struct {
	Parts  []SendPart `json:"parts"`
	Model  string     `json:"model,omitempty"`
	Agent  string     `json:"agent,omitempty"`
}

type SendPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type QuestionReplyRequest struct {
	Answers [][]string `json:"answers"`
}

type Provider struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Models      []ProviderModel `json:"models,omitempty"`
	Description string         `json:"description,omitempty"`
	Status      string         `json:"status,omitempty"`
}

type ProviderModel struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ProviderID string `json:"providerID,omitempty"`
}

type ProvidersResponse struct {
	All       []Provider      `json:"all"`
	Default   Provider        `json:"default"`
	Connected []string        `json:"connected"`
}
