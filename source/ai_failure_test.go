package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestAIProcessStopsAfterConnectionFailure(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "upstream unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	st := &Store{
		data:  map[string]map[uint32]*Email{},
		dirty: make(chan string, 16),
	}
	cm := &ConfigManager{
		cfg: &Config{AI: AIConfig{
			Enabled:  true,
			BaseURL:  srv.URL,
			APIKey:   "test-key",
			Model:    "test-model",
			Classify: true,
		}},
		changed: make(chan struct{}, 1),
	}
	ai := newAIService(cm, st)

	first := &Email{
		ID:        makeID("mb", 1),
		MailboxID: "mb",
		UID:       1,
		Subject:   "test notification",
		Text:      "body",
		AIStatus:  "pending",
	}
	st.Upsert(first)

	ai.process(first.ID)

	if got := requests.Load(); got != 1 {
		t.Fatalf("AI requests after connection failure = %d, want 1", got)
	}
	if !ai.paused.Load() {
		t.Fatal("AI queue was not paused after connection failure")
	}
	if got := st.Get(first.ID).AIStatus; got != "error" {
		t.Fatalf("failed email status = %q, want error", got)
	}

	second := &Email{
		ID:        makeID("mb", 2),
		MailboxID: "mb",
		UID:       2,
		Subject:   "another notification",
		Text:      "body",
		AIStatus:  "pending",
	}
	st.Upsert(second)
	ai.process(second.ID)

	if got := requests.Load(); got != 1 {
		t.Fatalf("paused AI queue made %d requests, want 1", got)
	}
	if got := st.Get(second.ID).AIStatus; got != "pending" {
		t.Fatalf("untouched email status = %q, want pending", got)
	}

	ai.ResumePending()
	if ai.paused.Load() {
		t.Fatal("AI queue remained paused after resume")
	}
	select {
	case got := <-ai.queue:
		if got != second.ID {
			t.Fatalf("resumed queue item = %q, want %q", got, second.ID)
		}
	default:
		t.Fatal("pending email was not queued after resume")
	}
}

func TestMergeFetchedDoesNotRetryFailedAI(t *testing.T) {
	st := &Store{
		data: map[string]map[uint32]*Email{
			"mb": {
				1: {
					ID:        makeID("mb", 1),
					MailboxID: "mb",
					UID:       1,
					AIStatus:  "error",
				},
				2: {
					ID:        makeID("mb", 2),
					MailboxID: "mb",
					UID:       2,
					AIStatus:  "pending",
				},
			},
		},
		dirty: make(chan string, 16),
	}
	fetched := []*Email{
		{ID: makeID("mb", 1), MailboxID: "mb", UID: 1},
		{ID: makeID("mb", 2), MailboxID: "mb", UID: 2},
	}

	_, needAI := st.MergeFetched("mb", fetched)

	if len(needAI) != 1 {
		t.Fatalf("emails queued for AI = %d, want 1", len(needAI))
	}
	if got := needAI[0].UID; got != 2 {
		t.Fatalf("queued email UID = %d, want 2 (failed email must not be retried)", got)
	}
}
