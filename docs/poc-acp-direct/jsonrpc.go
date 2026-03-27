package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// JSON-RPC 2.0 types

type Request struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// Transport handles ndjson read/write over stdio pipes.
type Transport struct {
	writer  io.Writer
	scanner *bufio.Scanner
	nextID  atomic.Int64
	mu      sync.Mutex // protects writer

	// pending tracks request ID → response channel
	pending   map[int64]chan *Response
	pendingMu sync.Mutex

	// NotificationHandler is called for incoming notifications
	NotificationHandler func(method string, params json.RawMessage)
}

func NewTransport(r io.Reader, w io.Writer) *Transport {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB line buffer
	return &Transport{
		writer:  w,
		scanner: s,
		pending: make(map[int64]chan *Response),
	}
}

// Send sends a JSON-RPC request and waits for the response.
func (t *Transport) Send(method string, params interface{}) (json.RawMessage, error) {
	id := t.nextID.Add(1)
	req := Request{JSONRPC: "2.0", ID: id, Method: method, Params: params}

	ch := make(chan *Response, 1)
	t.pendingMu.Lock()
	t.pending[id] = ch
	t.pendingMu.Unlock()

	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')

	t.mu.Lock()
	_, err = t.writer.Write(data)
	t.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	resp := <-ch
	if resp.Error != nil {
		return nil, resp.Error
	}
	return resp.Result, nil
}

// ReadLoop reads from the scanner and dispatches responses/notifications.
// Blocks until EOF or error. Call in a goroutine.
func (t *Transport) ReadLoop() error {
	for t.scanner.Scan() {
		line := t.scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Peek to determine if it's a response (has "id") or notification (has "method", no "id")
		var peek struct {
			ID     *int64 `json:"id"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal(line, &peek); err != nil {
			continue
		}

		if peek.ID != nil && peek.Method == "" {
			// Response
			var resp Response
			if err := json.Unmarshal(line, &resp); err != nil {
				continue
			}
			t.pendingMu.Lock()
			if ch, ok := t.pending[*resp.ID]; ok {
				delete(t.pending, *resp.ID)
				ch <- &resp
			}
			t.pendingMu.Unlock()
		} else if peek.Method != "" && peek.ID == nil {
			// Notification
			var notif Notification
			if err := json.Unmarshal(line, &notif); err != nil {
				continue
			}
			if t.NotificationHandler != nil {
				t.NotificationHandler(notif.Method, notif.Params)
			}
		} else if peek.ID != nil && peek.Method != "" {
			// Server-initiated request (e.g. requestPermission)
			var req struct {
				ID     int64           `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if err := json.Unmarshal(line, &req); err != nil {
				continue
			}
			// Auto-approve (we use --trust-all-tools, but handle just in case)
			t.respondToServer(req.ID, map[string]interface{}{
				"outcome": map[string]string{"outcome": "approved"},
			})
		}
	}
	return t.scanner.Err()
}

func (t *Transport) respondToServer(id int64, result interface{}) {
	resp := struct {
		JSONRPC string      `json:"jsonrpc"`
		ID      int64       `json:"id"`
		Result  interface{} `json:"result"`
	}{JSONRPC: "2.0", ID: id, Result: result}

	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	data = append(data, '\n')
	t.mu.Lock()
	t.writer.Write(data)
	t.mu.Unlock()
}
