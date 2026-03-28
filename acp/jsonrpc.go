package acp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// rpcRequest is a JSON-RPC 2.0 request.
type rpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// rpcResponse is a JSON-RPC 2.0 response.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC error.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// Transport handles ndjson JSON-RPC 2.0 over stdio pipes.
type Transport struct {
	writer  io.Writer
	scanner *bufio.Scanner
	nextID  atomic.Int64
	mu      sync.Mutex // protects writer

	pending   map[int64]chan *rpcResponse
	pendingMu sync.Mutex

	// OnNotification is called for incoming notifications.
	OnNotification func(method string, params json.RawMessage)
}

// NewTransport creates a transport over the given reader/writer (typically stdout/stdin of a child process).
func NewTransport(r io.Reader, w io.Writer) *Transport {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 1024*1024), 10*1024*1024)
	return &Transport{
		writer:  w,
		scanner: s,
		pending: make(map[int64]chan *rpcResponse),
	}
}

// Send sends a JSON-RPC request and blocks until the response arrives.
func (t *Transport) Send(method string, params interface{}) (json.RawMessage, error) {
	id := t.nextID.Add(1)
	ch := make(chan *rpcResponse, 1)

	t.pendingMu.Lock()
	t.pending[id] = ch
	t.pendingMu.Unlock()

	data, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
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

// ReadLoop reads ndjson lines and dispatches responses and notifications. Blocks until EOF.
func (t *Transport) ReadLoop() error {
	for t.scanner.Scan() {
		line := t.scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var peek struct {
			ID     *int64 `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(line, &peek) != nil {
			continue
		}

		switch {
		case peek.ID != nil && peek.Method == "":
			// Response to our request
			var resp rpcResponse
			if json.Unmarshal(line, &resp) != nil {
				continue
			}
			t.pendingMu.Lock()
			if ch, ok := t.pending[*resp.ID]; ok {
				delete(t.pending, *resp.ID)
				ch <- &resp
			}
			t.pendingMu.Unlock()

		case peek.Method != "" && peek.ID == nil:
			// Notification
			var notif struct {
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if json.Unmarshal(line, &notif) != nil {
				continue
			}
			if t.OnNotification != nil {
				t.OnNotification(notif.Method, notif.Params)
			}

		case peek.ID != nil && peek.Method != "":
			// Server-initiated request (e.g. permission) — auto-approve
			var req struct {
				ID int64 `json:"id"`
			}
			if json.Unmarshal(line, &req) != nil {
				continue
			}
			t.respond(req.ID, map[string]interface{}{
				"outcome": map[string]string{"outcome": "approved"},
			})
		}
	}
	return t.scanner.Err()
}

func (t *Transport) respond(id int64, result interface{}) {
	data, err := json.Marshal(struct {
		JSONRPC string      `json:"jsonrpc"`
		ID      int64       `json:"id"`
		Result  interface{} `json:"result"`
	}{JSONRPC: "2.0", ID: id, Result: result})
	if err != nil {
		return
	}
	data = append(data, '\n')
	t.mu.Lock()
	_, _ = t.writer.Write(data)
	t.mu.Unlock()
}
