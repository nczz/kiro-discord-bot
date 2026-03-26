package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 0}, // no global timeout; per-request via context
	}
}

// AgentStatus is the response from GET /agents/:name
type AgentStatus struct {
	Name      string `json:"name"`
	State     string `json:"state"`
	SessionID string `json:"sessionId"`
	LastError string `json:"lastError"`
	LastText  string `json:"lastText"`
}

// StartAgent starts a new kiro agent via POST /agents
func (c *Client) StartAgent(name, kiroCLI, cwd, model string) (*AgentStatus, error) {
	args := []string{"acp", "--trust-all-tools"}
	if model != "" {
		args = append(args, "--model", model)
	}
	body, _ := json.Marshal(map[string]any{
		"type":    "kiro",
		"name":    name,
		"command": kiroCLI,
		"args":    args,
		"cwd":     cwd,
	})
	resp, err := c.httpClient.Post(c.baseURL+"/agents", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("start agent failed %d: %s", resp.StatusCode, b)
	}
	var s AgentStatus
	return &s, json.NewDecoder(resp.Body).Decode(&s)
}

// GetAgent returns agent status, error if not found (404)
func (c *Client) GetAgent(name string) (*AgentStatus, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/agents/" + name)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("not_found")
	}
	var s AgentStatus
	return &s, json.NewDecoder(resp.Body).Decode(&s)
}

// StopAgent stops an agent via DELETE /agents/:name
func (c *Client) StopAgent(name string) error {
	req, _ := http.NewRequest(http.MethodDelete, c.baseURL+"/agents/"+name, nil)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// CancelAgent cancels current work via POST /agents/:name/cancel
func (c *Client) CancelAgent(name string) error {
	resp, err := c.httpClient.Post(c.baseURL+"/agents/"+name+"/cancel", "application/json", nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// AskResult is the final result from ask
type AskResult struct {
	Response   string `json:"response"`
	StopReason string `json:"stopReason"`
	State      string `json:"state"`
}

// Ask sends a prompt and waits for the full response (no streaming)
func (c *Client) Ask(ctx context.Context, name, prompt string) (*AskResult, error) {
	body, _ := json.Marshal(map[string]string{"prompt": prompt})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/agents/"+name+"/ask",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ask failed %d: %s", resp.StatusCode, b)
	}
	var r AskResult
	return &r, json.NewDecoder(resp.Body).Decode(&r)
}

// AskStream sends a prompt and streams chunks via onChunk callback.
// Returns the final AskResult when done.
func (c *Client) AskStream(ctx context.Context, name, prompt string, onChunk func(string)) (*AskResult, error) {
	body, _ := json.Marshal(map[string]string{"prompt": prompt})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/agents/"+name+"/ask?stream=true",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return parseSSE(resp.Body, onChunk)
}

// parseSSE reads text/event-stream and calls onChunk for each chunk event.
func parseSSE(r io.Reader, onChunk func(string)) (*AskResult, error) {
	scanner := bufio.NewScanner(r)
	var eventType, dataLine string

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			eventType = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			dataLine = strings.TrimPrefix(line, "data: ")
		case line == "":
			// dispatch event
			if eventType == "chunk" {
				var v struct {
					Chunk string `json:"chunk"`
				}
				if json.Unmarshal([]byte(dataLine), &v) == nil && onChunk != nil {
					onChunk(v.Chunk)
				}
			} else if eventType == "done" {
				var result AskResult
				if err := json.Unmarshal([]byte(dataLine), &result); err != nil {
					return nil, err
				}
				return &result, nil
			} else if eventType == "error" {
				var e struct {
					Error string `json:"error"`
				}
				json.Unmarshal([]byte(dataLine), &e)
				return nil, fmt.Errorf("agent error: %s", e.Error)
			}
			eventType, dataLine = "", ""
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("stream ended without done event")
}

// WaitUntilIdle polls agent status until idle or timeout
func (c *Client) WaitUntilIdle(name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s, err := c.GetAgent(name)
		if err != nil {
			return err
		}
		if s.State == "idle" {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("agent not idle after %s", timeout)
}
