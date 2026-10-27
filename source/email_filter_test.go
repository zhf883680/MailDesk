package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func testEmailServer(t *testing.T) (*Server, *Store) {
	t.Helper()
	dir := t.TempDir()
	cm, err := newConfigManager(dir)
	if err != nil {
		t.Fatalf("newConfigManager: %v", err)
	}
	st, err := newStore(filepath.Join(dir, "emails"))
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	return newServer(cm, st, nil, nil, nil, nil), st
}

func TestImportantFilterExcludesHandledEmails(t *testing.T) {
	s, st := testEmailServer(t)
	st.Upsert(&Email{ID: "mb:1", MailboxID: "mb", UID: 1, Subject: "Handled", Importance: "high", Handled: true})
	st.Upsert(&Email{ID: "mb:2", MailboxID: "mb", UID: 2, Subject: "Open", Importance: "high"})

	req := httptest.NewRequest(http.MethodGet, "/api/emails?filter=important", nil)
	rec := httptest.NewRecorder()
	s.listEmails(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Data []EmailSummary `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Data) != 1 || response.Data[0].Subject != "Open" {
		t.Fatalf("important emails = %#v, want only the unhandled email", response.Data)
	}
}

func TestImportantStatsExcludeHandledEmails(t *testing.T) {
	s, st := testEmailServer(t)
	st.Upsert(&Email{ID: "mb:1", MailboxID: "mb", UID: 1, Importance: "high", Handled: true})
	st.Upsert(&Email{ID: "mb:2", MailboxID: "mb", UID: 2, Importance: "high"})

	if got := s.stats().High; got != 1 {
		t.Fatalf("high count = %d, want 1", got)
	}
}
