package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func postAI(t *testing.T, s *Server, path, body string, fn func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	fn(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s status = %d, body = %s", path, rec.Code, rec.Body.String())
	}
	return rec
}

func TestAIQueueIgnoreOnlyAffectsFailures(t *testing.T) {
	s, st := testAIQueueServer(t)
	st.Upsert(&Email{ID: "mb:1", MailboxID: "mb", UID: 1, AIStatus: "error", AIError: "pure image"})
	st.Upsert(&Email{ID: "mb:2", MailboxID: "mb", UID: 2, AIStatus: "pending"})

	rec := postAI(t, s, "/api/ai/ignore", `{"ids":["mb:1","mb:2"]}`, s.ignoreAIQueue)
	var response struct {
		Data struct {
			Ignored int `json:"ignored"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if response.Data.Ignored != 1 {
		t.Fatalf("ignored = %d, want 1", response.Data.Ignored)
	}
	if got := st.Get("mb:1").AIStatus; got != "ignored" {
		t.Fatalf("failed email status = %q, want ignored", got)
	}
	if got := st.Get("mb:2").AIStatus; got != "pending" {
		t.Fatalf("pending email status = %q, want untouched", got)
	}

	q := s.aiQueue()
	if q.Counts["error"] != 0 || q.Counts["ignored"] != 1 {
		t.Fatalf("counts = %#v", q.Counts)
	}
	for _, item := range q.Items {
		if item.AIStatus == "error" {
			t.Fatalf("ignored email still reported as failure: %#v", item)
		}
	}
	if got := s.stats().FailedAI; got != 0 {
		t.Fatalf("stats ai_error = %d, want 0 after ignoring", got)
	}
}

func TestAIQueueIgnoreAllFailures(t *testing.T) {
	s, st := testAIQueueServer(t)
	st.Upsert(&Email{ID: "mb:1", MailboxID: "mb", UID: 1, AIStatus: "error"})
	st.Upsert(&Email{ID: "mb:2", MailboxID: "mb", UID: 2, AIStatus: "error"})
	st.Upsert(&Email{ID: "mb:3", MailboxID: "mb", UID: 3, AIStatus: "done"})

	postAI(t, s, "/api/ai/ignore", "", s.ignoreAIQueue)
	for _, id := range []string{"mb:1", "mb:2"} {
		if got := st.Get(id).AIStatus; got != "ignored" {
			t.Fatalf("%s status = %q, want ignored", id, got)
		}
	}
	if got := st.Get("mb:3").AIStatus; got != "done" {
		t.Fatalf("done email status = %q, want untouched", got)
	}
}

func TestRetryAllSkipsIgnored(t *testing.T) {
	s, st := testAIQueueServer(t)
	st.Upsert(&Email{ID: "mb:1", MailboxID: "mb", UID: 1, AIStatus: "ignored"})

	postAI(t, s, "/api/ai/retry", `{"status":"all"}`, s.retryAIQueue)
	if got := st.Get("mb:1").AIStatus; got != "ignored" {
		t.Fatalf("ignored email status = %q, want kept ignored", got)
	}
}

func TestIsTimeoutError(t *testing.T) {
	timeout := &aiUnavailableError{err: &url.Error{Op: "Post", URL: "http://x", Err: context.DeadlineExceeded}}
	if !isTimeoutError(timeout) {
		t.Fatal("wrapped context deadline should be a timeout")
	}
	if isTimeoutError(errors.New("boom")) {
		t.Fatal("plain error should not be a timeout")
	}
}

func TestTimeoutRetryOnlyOnce(t *testing.T) {
	s, st := testAIQueueServer(t)
	st.Upsert(&Email{ID: "mb:1", MailboxID: "mb", UID: 1, AIStatus: "running"})

	if !s.ai.retryAfterTimeout("mb:1") {
		t.Fatal("first timeout should schedule a retry")
	}
	e := st.Get("mb:1")
	if e.AIStatus != "pending" || e.AITimeoutRetries != 1 {
		t.Fatalf("status = %q, retries = %d, want pending/1", e.AIStatus, e.AITimeoutRetries)
	}
	if e.AIError != "" {
		t.Fatalf("ai_error should stay empty while waiting to retry: %q", e.AIError)
	}
	if s.ai.retryAfterTimeout("mb:1") {
		t.Fatal("second timeout should not schedule another retry")
	}
}
