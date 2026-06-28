package server

import (
	"net/http/httptest"
	"testing"
)

func TestConversationIDFromHeader(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.Header.Set(conversationIDHeader, " conv-123 ")
	if got := conversationIDFromHeader(req); got != "conv-123" {
		t.Fatalf("conversationIDFromHeader() = %q, want conv-123", got)
	}
	if got := conversationIDFromHeader(nil); got != "" {
		t.Fatalf("conversationIDFromHeader(nil) = %q, want empty", got)
	}
}
