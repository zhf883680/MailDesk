package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestMergeFetchedPreservesCachedContentWhenBodyWasNotDownloaded(t *testing.T) {
	st, err := newStore(filepath.Join(t.TempDir(), "emails"))
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	old := &Email{
		ID:         "mb:7",
		MailboxID:  "mb",
		UID:        7,
		Subject:    "Old subject",
		Snippet:    "cached preview",
		Text:       "cached body",
		HTML:       "<p>cached body</p>",
		Markdown:   "cached body",
		BodyLoaded: true,
		Seen:       false,
		Flagged:    false,
		AIStatus:   "done",
		Summary:    "cached summary",
		Attachments: []Attachment{{
			Index: 0, Filename: "invoice.pdf", ContentType: "application/pdf", Size: 42,
		}},
	}
	st.Upsert(old)

	headerOnly := &Email{
		ID:        old.ID,
		MailboxID: old.MailboxID,
		UID:       old.UID,
		Subject:   "Updated subject",
		Date:      time.Now(),
		Seen:      true,
		Flagged:   true,
	}

	added, _ := st.MergeFetched("mb", []*Email{headerOnly})
	if len(added) != 0 {
		t.Fatalf("header refresh should not add an existing email: %d", len(added))
	}

	got := st.Get(old.ID)
	if got == nil {
		t.Fatal("merged email not found")
	}
	if !got.Seen {
		t.Fatal("read state from the mailbox header was not updated")
	}
	if got.Flagged != old.Flagged {
		t.Fatalf("locally managed flag state was replaced: flagged=%t", got.Flagged)
	}
	if got.Subject != "Updated subject" {
		t.Fatalf("header subject was not updated: %q", got.Subject)
	}
	if got.Snippet != old.Snippet || got.Text != old.Text || got.HTML != old.HTML || got.Markdown != old.Markdown {
		t.Fatalf("cached body was replaced: %#v", got)
	}
	if !got.BodyLoaded {
		t.Fatal("cached body marker was lost")
	}
	if len(got.Attachments) != 1 || got.Attachments[0].Filename != "invoice.pdf" {
		t.Fatalf("cached attachments were replaced: %#v", got.Attachments)
	}
	if got.Summary != old.Summary || got.AIStatus != old.AIStatus {
		t.Fatalf("cached AI data was replaced: %#v", got)
	}
}

func TestLoadedBodyUIDsIncludesLegacyBodiesAndExplicitlyLoadedMessages(t *testing.T) {
	st, err := newStore(filepath.Join(t.TempDir(), "emails"))
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	st.Upsert(&Email{ID: "mb:1", MailboxID: "mb", UID: 1, Text: "legacy body"})
	st.Upsert(&Email{ID: "mb:2", MailboxID: "mb", UID: 2, BodyLoaded: true})
	st.Upsert(&Email{ID: "mb:3", MailboxID: "mb", UID: 3})

	got := st.LoadedBodyUIDs("mb")
	if !got[1] || !got[2] {
		t.Fatalf("loaded bodies were not detected: %#v", got)
	}
	if got[3] {
		t.Fatalf("message with no cached body must be fetched: %#v", got)
	}
}

func TestMergeFetchedUsesNewBodyWhenItWasDownloaded(t *testing.T) {
	st, err := newStore(filepath.Join(t.TempDir(), "emails"))
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	st.Upsert(&Email{
		ID: "mb:8", MailboxID: "mb", UID: 8, BodyLoaded: true,
		Snippet: "old preview", Text: "old body", HTML: "<p>old body</p>",
	})

	fetched := &Email{
		ID: "mb:8", MailboxID: "mb", UID: 8,
		Snippet: "new preview", Text: "new body", HTML: "<p>new body</p>",
		BodyLoaded: true, bodyFetched: true,
	}
	st.MergeFetched("mb", []*Email{fetched})

	got := st.Get("mb:8")
	if got == nil {
		t.Fatal("merged email not found")
	}
	if got.Snippet != "new preview" || got.Text != "new body" || got.HTML != "<p>new body</p>" {
		t.Fatalf("downloaded body was not stored: %#v", got)
	}
}
