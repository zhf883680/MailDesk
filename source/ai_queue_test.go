package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testAIQueueServer(t *testing.T) (*Server, *Store) {
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
	return newServer(cm, st, newAIService(cm, st), nil, nil, nil), st
}

func TestAIQueuePutsFailuresFirstAndCarriesError(t *testing.T) {
	s, st := testAIQueueServer(t)
	now := time.Now()
	st.Upsert(&Email{ID: "mb:1", MailboxID: "mb", UID: 1, Subject: "Failed one", AIStatus: "error", AIError: "invalid character 'è'", AIAt: now.Add(-time.Minute)})
	st.Upsert(&Email{ID: "mb:2", MailboxID: "mb", UID: 2, Subject: "Queued one", AIStatus: "pending", Date: now})
	st.Upsert(&Email{ID: "mb:3", MailboxID: "mb", UID: 3, Subject: "Done one", AIStatus: "done", AIAt: now})

	q := s.aiQueue()
	if q.Counts["error"] != 1 || q.Counts["pending"] != 1 || q.Counts["done"] != 1 {
		t.Fatalf("counts = %#v", q.Counts)
	}
	if len(q.Items) != 3 {
		t.Fatalf("items = %d, want 3", len(q.Items))
	}
	if q.Items[0].ID != "mb:1" {
		t.Fatalf("first item = %s, want failed email first", q.Items[0].ID)
	}
	if q.Items[0].AIError != "invalid character 'è'" {
		t.Fatalf("ai_error not exposed: %q", q.Items[0].AIError)
	}
}

func TestAIQueueRetryMarksPending(t *testing.T) {
	s, st := testAIQueueServer(t)
	st.Upsert(&Email{ID: "mb:1", MailboxID: "mb", UID: 1, Subject: "Failed", AIStatus: "error", AIError: "boom"})

	body := strings.NewReader(`{"ids":["mb:1"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/ai/retry", body)
	rec := httptest.NewRecorder()
	s.retryAIQueue(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Data struct {
			Queued int `json:"queued"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if response.Data.Queued != 1 {
		t.Fatalf("queued = %d, want 1", response.Data.Queued)
	}
	e := st.Get("mb:1")
	if e == nil || e.AIStatus != "pending" {
		t.Fatalf("email status = %v, want pending", e)
	}
}

func TestStatsCountsFailedAI(t *testing.T) {
	s, st := testAIQueueServer(t)
	st.Upsert(&Email{ID: "mb:1", MailboxID: "mb", UID: 1, AIStatus: "error"})
	st.Upsert(&Email{ID: "mb:2", MailboxID: "mb", UID: 2, AIStatus: "pending"})
	if got := s.stats().FailedAI; got != 1 {
		t.Fatalf("stats ai_error = %d, want 1", got)
	}
}
