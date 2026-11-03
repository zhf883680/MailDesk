package main

import (
	"testing"
	"time"
)

func TestBackgroundSyncInterval(t *testing.T) {
	at := func(hour, minute int) time.Time {
		return time.Date(2026, time.October, 24, hour, minute, 0, 0, time.Local)
	}
	tests := []struct {
		name   string
		now    time.Time
		cfg    Config
		expect time.Duration
	}{
		{"daytime configured interval", at(12, 0), Config{SyncMinutes: 5}, 5 * time.Minute},
		{"daytime bark forces one minute", at(12, 0), Config{SyncMinutes: 30, Forward: ForwardConfig{Enabled: true}}, time.Minute},
		{"nighttime ignores configured interval", at(22, 0), Config{SyncMinutes: 5}, time.Hour},
		{"nighttime ignores bark", at(23, 30), Config{SyncMinutes: 5, Forward: ForwardConfig{Enabled: true}}, time.Hour},
		{"early morning is nighttime", at(7, 59), Config{SyncMinutes: 5, Forward: ForwardConfig{Enabled: true}}, time.Hour},
		{"morning resumes daytime interval", at(8, 0), Config{SyncMinutes: 5, Forward: ForwardConfig{Enabled: true}}, time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := backgroundSyncInterval(tt.now, &tt.cfg); got != tt.expect {
				t.Fatalf("backgroundSyncInterval() = %v, want %v", got, tt.expect)
			}
		})
	}
}

func TestSyncSleepDurationStopsAtPeriodBoundary(t *testing.T) {
	at := func(hour, minute int) time.Time {
		return time.Date(2026, time.October, 24, hour, minute, 0, 0, time.Local)
	}
	tests := []struct {
		name   string
		now    time.Time
		cfg    Config
		expect time.Duration
	}{
		{"daytime interval before night boundary", at(21, 30), Config{SyncMinutes: 1440}, 30 * time.Minute},
		{"nighttime interval before morning boundary", at(7, 30), Config{SyncMinutes: 5, Forward: ForwardConfig{Enabled: true}}, 30 * time.Minute},
		{"regular daytime interval", at(12, 0), Config{SyncMinutes: 5}, 5 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := syncSleepDuration(tt.now, &tt.cfg); got != tt.expect {
				t.Fatalf("syncSleepDuration() = %v, want %v", got, tt.expect)
			}
		})
	}
}
