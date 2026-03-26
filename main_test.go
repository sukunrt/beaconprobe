package main

import (
	"log/slog"
	"testing"
	"time"
)

func TestReplaceTimeAttr(t *testing.T) {
	ts := time.Date(2026, 3, 20, 12, 34, 56, 123456789, time.UTC)
	attr := slog.Time(slog.TimeKey, ts)

	result := replaceTimeAttr(nil, attr)

	want := "2026-03-20T12:34:56.123456Z"
	if result.Value.String() != want {
		t.Fatalf("got %q, want %q", result.Value.String(), want)
	}
}

func TestReplaceTimeAttrNonTimeKey(t *testing.T) {
	attr := slog.String("foo", "bar")
	result := replaceTimeAttr(nil, attr)
	if result.Value.String() != "bar" {
		t.Fatalf("non-time attr should be unchanged, got %q", result.Value.String())
	}
}
