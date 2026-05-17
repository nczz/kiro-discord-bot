package acp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestTransportServerRequestDefaultDeny(t *testing.T) {
	var out bytes.Buffer
	tr := NewTransport(strings.NewReader(`{"jsonrpc":"2.0","id":7,"method":"permission/request","params":{}}`+"\n"), &out, 0)
	if err := tr.ReadLoop(); err != nil {
		t.Fatalf("ReadLoop: %v", err)
	}

	var resp struct {
		ID     int `json:"id"`
		Result struct {
			Outcome struct {
				Outcome string `json:"outcome"`
			} `json:"outcome"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("response json: %v", err)
	}
	if resp.ID != 7 || resp.Result.Outcome.Outcome != "denied" {
		t.Fatalf("unexpected response: %s", out.String())
	}
}

func TestTransportServerRequestHandler(t *testing.T) {
	var out bytes.Buffer
	tr := NewTransport(strings.NewReader(`{"jsonrpc":"2.0","id":8,"method":"permission/request","params":{}}`+"\n"), &out, 0)
	tr.OnRequest = func(method string, _ json.RawMessage) interface{} {
		if method != "permission/request" {
			t.Fatalf("method = %q", method)
		}
		return ApproveRequestResult()
	}
	if err := tr.ReadLoop(); err != nil {
		t.Fatalf("ReadLoop: %v", err)
	}

	if !strings.Contains(out.String(), `"approved"`) {
		t.Fatalf("expected approved response, got %s", out.String())
	}
}
