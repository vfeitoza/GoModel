package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"

	"gomodel/internal/routingstate"
)

func newRoutingStateHandler(t *testing.T) *Handler {
	t.Helper()
	service, err := routingstate.NewService(&routingStateMemoryStore{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return NewHandler(nil, nil, WithRoutingState(service))
}

type routingStateMemoryStore struct{ entries map[string]routingstate.Entry }

func (m *routingStateMemoryStore) List(context.Context) ([]routingstate.Entry, error) {
	result := make([]routingstate.Entry, 0, len(m.entries))
	for _, entry := range m.entries { result = append(result, entry) }
	return result, nil
}
func (m *routingStateMemoryStore) Upsert(_ context.Context, entry routingstate.Entry) error { if m.entries == nil { m.entries = map[string]routingstate.Entry{} }; m.entries[entry.Key] = entry; return nil }
func (m *routingStateMemoryStore) Delete(_ context.Context, key string) error { delete(m.entries, key); return nil }
func (m *routingStateMemoryStore) Close() error { return nil }

func TestListRoutingState_Empty(t *testing.T) {
	h := newRoutingStateHandler(t)
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/admin/routing-state", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if err := h.ListRoutingState(c); err != nil { t.Fatalf("ListRoutingState() error = %v", err) }
	if rec.Code != http.StatusOK { t.Fatalf("status = %d, want 200", rec.Code) }
	if rec.Body.String() != "[]\n" { t.Fatalf("body = %q, want []", rec.Body.String()) }
}

func TestUpsertRoutingState_Provider(t *testing.T) {
	h := newRoutingStateHandler(t)
	body, _ := json.Marshal(map[string]any{"kind": "provider", "provider_name": "anthropic_a", "enabled": false, "reason": "429"})
	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/admin/routing-state", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if err := h.UpsertRoutingState(c); err != nil { t.Fatalf("UpsertRoutingState() error = %v", err) }
	if rec.Code != http.StatusOK { t.Fatalf("status = %d, want 200", rec.Code) }
}

func TestUpsertRoutingState_InvalidMissingEnabled(t *testing.T) {
	h := newRoutingStateHandler(t)
	body, _ := json.Marshal(map[string]any{"kind": "provider", "provider_name": "anthropic_a"})
	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/admin/routing-state", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	_ = h.UpsertRoutingState(c)
	if rec.Code != http.StatusBadRequest { t.Fatalf("status = %d, want 400", rec.Code) }
}

func TestDeleteRoutingState(t *testing.T) {
	h := newRoutingStateHandler(t)
	body, _ := json.Marshal(map[string]any{"kind": "provider", "provider_name": "anthropic_a", "enabled": false})
	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/admin/routing-state", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if err := h.UpsertRoutingState(c); err != nil { t.Fatalf("UpsertRoutingState() error = %v", err) }

	deleteBody, _ := json.Marshal(map[string]any{"key": "anthropic_a"})
	req = httptest.NewRequest(http.MethodDelete, "/admin/routing-state", bytes.NewReader(deleteBody))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	if err := h.DeleteRoutingState(c); err != nil { t.Fatalf("DeleteRoutingState() error = %v", err) }
	if rec.Code != http.StatusNoContent { t.Fatalf("status = %d, want 204", rec.Code) }
}
