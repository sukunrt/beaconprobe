package main

import (
	"log/slog"
	"testing"
	"time"
)

func TestParseSubnets(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []uint64
		wantErr bool
	}{
		{name: "single", input: "0", want: []uint64{0}},
		{name: "multiple", input: "0,1,2,3", want: []uint64{0, 1, 2, 3}},
		{name: "with spaces", input: " 5 , 10 , 63 ", want: []uint64{5, 10, 63}},
		{name: "empty parts", input: "1,,2", want: []uint64{1, 2}},
		{name: "all empty", input: "", want: []uint64{}},
		{name: "invalid", input: "abc", wantErr: true},
		{name: "negative", input: "-1", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSubnets(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got[%d] = %d, want %d", i, got[i], tt.want[i])
				}
			}
		})
	}
}

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
