package opencode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Client communicates with a single OpenCode server instance.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{},
	}
}

func (c *Client) ListSessions(ctx context.Context, directory string) ([]Session, error) {
	endpoint := c.baseURL + "/session"
	if directory != "" {
		endpoint += "?directory=" + url.QueryEscape(directory)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list sessions: status %d: %s", resp.StatusCode, body)
	}
	var sessions []Session
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		return nil, err
	}
	return sessions, nil
}

// ListSessionChildren returns child sessions of the given parent session.
func (c *Client) ListSessionChildren(ctx context.Context, sessionID string) ([]Session, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/session/"+sessionID+"/children", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list session children: status %d: %s", resp.StatusCode, body)
	}
	var sessions []Session
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		return nil, err
	}
	return sessions, nil
}

func (c *Client) CreateSession(ctx context.Context, directory string) (*Session, error) {
	body, _ := json.Marshal(CreateSessionRequest{})
	endpoint := c.baseURL + "/session"
	if directory != "" {
		endpoint += "?directory=" + url.QueryEscape(directory)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create session: status %d: %s", resp.StatusCode, respBody)
	}
	var session Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, err
	}
	return &session, nil
}

// GetSession retrieves a session by ID. Returns an error if the session does
// not exist (e.g. was deleted).
func (c *Client) GetSession(ctx context.Context, sessionID string) (*Session, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/session/"+sessionID, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get session: status %d: %s", resp.StatusCode, body)
	}
	var session Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, err
	}
	return &session, nil
}

// IsSessionIdle checks if a session is idle by querying the status endpoint.
func (c *Client) IsSessionIdle(ctx context.Context, sessionID string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/session/status", nil)
	if err != nil {
		return false, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("session status: status %d", resp.StatusCode)
	}
	var statuses map[string]struct {
		Type string `json:"type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&statuses); err != nil {
		return false, err
	}
	st, ok := statuses[sessionID]
	if !ok {
		// Session not in status map — treat as idle (may have completed already)
		return true, nil
	}
	return st.Type == "idle", nil
}

func (c *Client) GetMessages(ctx context.Context, sessionID string) ([]Message, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/session/"+sessionID+"/message", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get messages: status %d: %s", resp.StatusCode, body)
	}
	var messages []Message
	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		return nil, err
	}
	return messages, nil
}

func (c *Client) SendMessage(ctx context.Context, sessionID string, content string, model string) error {
	var body []byte
	if model != "" {
		body, _ = json.Marshal(SendMessageRequestWithModel{
			Parts: []SendPart{{Type: "text", Text: content}},
			Model: model,
		})
	} else {
		body, _ = json.Marshal(SendMessageRequest{
			Parts: []SendPart{{Type: "text", Text: content}},
		})
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/session/"+sessionID+"/message", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("send message: status %d: %s", resp.StatusCode, respBody)
	}
	return nil
}

// ReplyToQuestion answers a pending question in OpenCode.
// Each answer is an array of strings (selected option labels).
func (c *Client) ReplyToQuestion(ctx context.Context, requestID string, answers [][]string) error {
	body, _ := json.Marshal(QuestionReplyRequest{Answers: answers})
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/question/"+requestID+"/reply", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("reply to question: status %d: %s", resp.StatusCode, respBody)
	}
	return nil
}

func (c *Client) DeleteSession(ctx context.Context, sessionID string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.baseURL+"/session/"+sessionID, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete session: status %d: %s", resp.StatusCode, respBody)
	}
	return nil
}

// SubscribeEvents opens an SSE connection to the OpenCode /event endpoint.
// OpenCode sends events as `data: {"type":"...", "properties":{...}}` lines.
// Blocks until context is cancelled or the stream ends.
func (c *Client) SubscribeEvents(ctx context.Context, handler func(rawJSON string)) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/event", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	// OpenCode can send large events (tool results etc), increase buffer
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			handler(data)
		}
	}
	return scanner.Err()
}

// GetProviders returns all available providers from the OpenCode server.
func (c *Client) GetProviders(ctx context.Context) (*ProvidersResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/provider", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get providers: status %d: %s", resp.StatusCode, body)
	}
	var providers ProvidersResponse
	if err := json.NewDecoder(resp.Body).Decode(&providers); err != nil {
		return nil, err
	}
	return &providers, nil
}

// GetConfigProviders returns providers with their default models from config.
func (c *Client) GetConfigProviders(ctx context.Context) (map[string][]ProviderModel, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/config/providers", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get config providers: status %d: %s", resp.StatusCode, body)
	}
	var result struct {
		Providers map[string][]ProviderModel `json:"providers"`
		Default   map[string]string          `json:"default"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Providers, nil
}
