package mcp

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestSuccessAndErrorResponse(t *testing.T) {
	respMap := SuccessResponse("all good")
	content, ok := respMap["content"].([]ContentBlock)
	if !ok || len(content) != 1 || content[0].Text != "all good" || content[0].Type != "text" {
		t.Fatalf("unexpected success response payload: %#v", respMap)
	}

	errResp := ErrorResponse("bad")
	errContent, ok := errResp["content"].([]ContentBlock)
	if !ok || len(errContent) != 1 || errContent[0].Text != "bad" {
		t.Fatalf("unexpected error response payload: %#v", errResp)
	}
}

func TestUnmarshalArgs(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
		ID   int    `json:"id"`
	}

	got, err := UnmarshalArgs[payload](map[string]any{
		"name": "alpha",
		"id":   42,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "alpha" || got.ID != 42 {
		t.Fatalf("unexpected payload: %#v", got)
	}

	_, err = UnmarshalArgs[payload](make(chan int))
	if err == nil {
		t.Fatalf("expected error for unsupported args type")
	}
	var jsonErr *json.UnsupportedTypeError
	if !errors.As(err, &jsonErr) {
		t.Fatalf("expected json unsupported type error, got %v", err)
	}
}
