package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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

func TestAdStatsAreIgnoredAndReadByDefault(t *testing.T) {
	s, st := testEmailServer(t)
	st.Upsert(&Email{
		ID: "mb:1", MailboxID: "mb", UID: 1, Subject: "Promotion",
		IsAd: true, Importance: "high", NeedAction: true,
	})
	st.Upsert(&Email{ID: "mb:2", MailboxID: "mb", UID: 2, Subject: "Important update"})

	got := s.stats()
	if got.Unread != 1 {
		t.Fatalf("unread count = %d, want 1", got.Unread)
	}
	if got.Ads != 0 {
		t.Fatalf("ad count = %d, want 0", got.Ads)
	}
	if got.High != 0 {
		t.Fatalf("high count = %d, want 0", got.High)
	}
	if got.NeedAction != 0 {
		t.Fatalf("need-action count = %d, want 0", got.NeedAction)
	}
}

func TestUnreadListTreatsAdsAsRead(t *testing.T) {
	s, st := testEmailServer(t)
	st.Upsert(&Email{ID: "mb:1", MailboxID: "mb", UID: 1, Subject: "Promotion", IsAd: true})
	st.Upsert(&Email{ID: "mb:2", MailboxID: "mb", UID: 2, Subject: "Important update"})

	req := httptest.NewRequest(http.MethodGet, "/api/emails?filter=unread", nil)
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
	if len(response.Data) != 1 || response.Data[0].Subject != "Important update" {
		t.Fatalf("unread emails = %#v, want only the non-ad email", response.Data)
	}
}

func TestExplicitlyUnignoredAdRemainsRead(t *testing.T) {
	s, st := testEmailServer(t)
	st.Upsert(&Email{
		ID: "mb:1", MailboxID: "mb", UID: 1, Subject: "Promotion",
		IsAd: true,
	})
	req := httptest.NewRequest(http.MethodPut, "/api/emails/mb:1/flags", strings.NewReader(`{"ad_ignored":false}`))
	rec := httptest.NewRecorder()
	s.setEmailFlags(rec, req, "mb:1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	got := s.stats()
	if got.Ads != 1 {
		t.Fatalf("ad count = %d, want 1 after an explicit unignore", got.Ads)
	}
	if got.Unread != 0 {
		t.Fatalf("unread count = %d, want 0", got.Unread)
	}
}
