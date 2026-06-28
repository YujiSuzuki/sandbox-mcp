package jsonrpc

import (
	"encoding/json"
	"testing"
)

func TestRequestUnmarshal(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	var req Request
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if req.JSONRPC != "2.0" {
		t.Errorf("JSONRPC = %q, want %q", req.JSONRPC, "2.0")
	}
	if req.Method != "tools/list" {
		t.Errorf("Method = %q, want %q", req.Method, "tools/list")
	}
	if req.ID != float64(1) {
		t.Errorf("ID = %v, want 1", req.ID)
	}
	if req.IsNotification() {
		t.Error("Expected non-notification request")
	}
	if len(req.Params) == 0 {
		t.Error("Expected non-empty Params")
	}
}

func TestNotificationDetection(t *testing.T) {
	input := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	var req Request
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if !req.IsNotification() {
		t.Error("Expected notification (no id)")
	}
}

func TestRequestUnmarshalStringID(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":"req-abc","method":"tools/call"}`
	var req Request
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if req.ID != "req-abc" {
		t.Errorf("ID = %v, want %q", req.ID, "req-abc")
	}
	if req.IsNotification() {
		t.Error("Expected non-notification for string ID")
	}
}

func TestNewResponse(t *testing.T) {
	resp := NewResponse(1, map[string]string{"key": "value"})
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var parsed Response
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if parsed.JSONRPC != "2.0" {
		t.Errorf("JSONRPC = %q, want %q", parsed.JSONRPC, "2.0")
	}
	if parsed.ID != float64(1) {
		t.Errorf("ID = %v, want 1", parsed.ID)
	}
	if parsed.Error != nil {
		t.Error("Expected no error")
	}
	resultMap, ok := parsed.Result.(map[string]any)
	if !ok {
		t.Fatalf("Result type = %T, want map[string]any", parsed.Result)
	}
	if resultMap["key"] != "value" {
		t.Errorf("Result[key] = %v, want %q", resultMap["key"], "value")
	}
}

func TestNewResponseStringID(t *testing.T) {
	resp := NewResponse("req-abc", "ok")
	if resp.ID != "req-abc" {
		t.Errorf("ID = %v, want %q", resp.ID, "req-abc")
	}
	if resp.Result != "ok" {
		t.Errorf("Result = %v, want %q", resp.Result, "ok")
	}
}

func TestNewErrorResponse(t *testing.T) {
	resp := NewErrorResponse(1, CodeMethodNotFound, "Method not found")
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var parsed Response
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if parsed.ID != float64(1) {
		t.Errorf("ID = %v, want 1", parsed.ID)
	}
	if parsed.Error == nil {
		t.Fatal("Expected error")
	}
	if parsed.Error.Code != CodeMethodNotFound {
		t.Errorf("Error.Code = %d, want %d", parsed.Error.Code, CodeMethodNotFound)
	}
	if parsed.Error.Message != "Method not found" {
		t.Errorf("Error.Message = %q, want %q", parsed.Error.Message, "Method not found")
	}
}
