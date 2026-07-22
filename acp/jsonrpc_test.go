package acp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestTransportServerRequestDefaultDeny(t *testing.T) {
	var out bytes.Buffer
	tr := NewTransport(strings.NewReader(`{"jsonrpc":"2.0","id":7,"method":"session/request_permission","params":{"options":[{"optionId":"allow-once","kind":"allow_once"},{"optionId":"reject-once","kind":"reject_once"}]}}`+"\n"), &out, 0)
	if err := tr.ReadLoop(); err != nil {
		t.Fatalf("ReadLoop: %v", err)
	}

	var resp struct {
		ID     int `json:"id"`
		Result struct {
			Outcome struct {
				Outcome  string `json:"outcome"`
				OptionID string `json:"optionId"`
			} `json:"outcome"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("response json: %v", err)
	}
	if resp.ID != 7 || resp.Result.Outcome.Outcome != "selected" || resp.Result.Outcome.OptionID != "reject-once" {
		t.Fatalf("unexpected response: %s", out.String())
	}
}

func TestTransportServerRequestHandler(t *testing.T) {
	var out bytes.Buffer
	tr := NewTransport(strings.NewReader(`{"jsonrpc":"2.0","id":8,"method":"session/request_permission","params":{"options":[{"optionId":"allow-once","kind":"allow_once"},{"optionId":"allow-always","kind":"allow_always"}]}}`+"\n"), &out, 0)
	tr.OnRequest = func(method string, params json.RawMessage) interface{} {
		if method != "session/request_permission" {
			t.Fatalf("method = %q", method)
		}
		return ApproveRequestResult(params)
	}
	if err := tr.ReadLoop(); err != nil {
		t.Fatalf("ReadLoop: %v", err)
	}

	if !strings.Contains(out.String(), `"outcome":"selected"`) || !strings.Contains(out.String(), `"optionId":"allow-once"`) {
		t.Fatalf("expected selected allow response, got %s", out.String())
	}
}
