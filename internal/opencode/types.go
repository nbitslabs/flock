package opencode

// OpenCode ACP API types

type Session struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

type SessionListResponse struct {
	Sessions []Session `json:"sessions"`
}

type CreateSessionRequest struct {
	Title string `json:"title,omitempty"`
}

type Message struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Role      string `json:"role"`
	Parts     []Part `json:"parts"`
	CreatedAt int64  `json:"createdAt"`
}

type Part struct {
	Type    string `json:"type"`
	Content string `json:"content,omitempty"`
	Text    string `json:"text,omitempty"`
}

type MessageListResponse struct {
	Messages []Message `json:"messages"`
}

type SendMessageRequest struct {
	Content string `json:"content"`
}

type Event struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}
