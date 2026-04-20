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
	Code    int              `json:"code"`
	Message string           `json:"message"`
	Data    json.RawMessage  `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	if len(e.Data) > 0 {
		return fmt.Sprintf("rpc error %d: %s | data: %s", e.Code, e.Message, string(e.Data))
	}
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

	// done is closed when ReadLoop exits; doneErr holds the reason.
	done    chan struct{}
	doneErr error

	// OnNotification is called for incoming notifications.
	OnNotification func(method string, params json.RawMessage)
}

// NewTransport creates a transport over the given reader/writer (typically stdout/stdin of a child process).
// maxBuffer sets the maximum token size for the line scanner (bytes). If <= 0, defaults to 64 MiB.
func NewTransport(r io.Reader, w io.Writer, maxBuffer int) *Transport {
	if maxBuffer <= 0 {
		maxBuffer = 64 * 1024 * 1024
	}
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 1024*1024), maxBuffer)
	return &Transport{
		writer:  w,
		scanner: s,
		pending: make(map[int64]chan *rpcResponse),
		done:    make(chan struct{}),
	}
}

// Send sends a JSON-RPC request and blocks until the response arrives or ReadLoop exits.
func (t *Transport) Send(method string, params interface{}) (json.RawMessage, error) {
	// Fast path: if ReadLoop already dead, fail immediately.
	select {
	case <-t.done:
		if t.doneErr != nil {
			return nil, fmt.Errorf("transport closed: %w", t.doneErr)
		}
		return nil, fmt.Errorf("transport closed")
	default:
	}

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
		t.pendingMu.Lock()
		delete(t.pending, id)
		t.pendingMu.Unlock()
		return nil, fmt.Errorf("write: %w", err)
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	case <-t.done:
		t.pendingMu.Lock()
		delete(t.pending, id)
		t.pendingMu.Unlock()
		if t.doneErr != nil {
			return nil, fmt.Errorf("transport closed: %w", t.doneErr)
		}
		return nil, fmt.Errorf("transport closed")
	}
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

	err := t.scanner.Err()
	t.doneErr = err
	close(t.done)
	t.failAllPending(err)
	return err
}

// failAllPending unblocks all pending Send() calls with an error.
func (t *Transport) failAllPending(reason error) {
	msg := "transport closed"
	if reason != nil {
		msg = reason.Error()
	}
	t.pendingMu.Lock()
	for id, ch := range t.pending {
		ch <- &rpcResponse{Error: &RPCError{Code: -1, Message: msg}}
		delete(t.pending, id)
	}
	t.pendingMu.Unlock()
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
