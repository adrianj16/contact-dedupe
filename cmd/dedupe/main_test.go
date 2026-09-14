package main

import (
	"encoding/csv"
	"strings"
	"testing"

	"github.com/adrianugas/contact-dedupe/dedupe"
)

func sample() []dedupe.Match {
	return []dedupe.Match{
		{SourceID: "1", MatchID: "501", Score: 0.94, Confidence: dedupe.High,
			Reasons: []string{"email: identical", "ZIP: identical"}},
		{SourceID: "2", MatchID: "502", Score: 0.71, Confidence: dedupe.Medium,
			Reasons: []string{"address: near-identical"}},
		{SourceID: "3", MatchID: "503", Score: 0.44, Confidence: dedupe.Low,
			Reasons: []string{"first name: initial is consistent (c / ciara)"}},
	}
}

// TestWriteCSVFormat pins the output columns to the format the brief asks for.
// A silent change here would break whatever consumes the file.
func TestWriteCSVFormat(t *testing.T) {
	var buf strings.Builder
	if err := writeCSV(&buf, sample()); err != nil {
		t.Fatalf("writeCSV: %v", err)
	}

	records, err := csv.NewReader(strings.NewReader(buf.String())).ReadAll()
	if err != nil {
		t.Fatalf("output is not valid CSV: %v", err)
	}
	if len(records) != 4 {
		t.Fatalf("got %d rows, want 1 header + 3 matches", len(records))
	}

	wantHeader := []string{"ContactID Source", "ContactID Match", "Accuracy", "Score"}
	for i, want := range wantHeader {
		if records[0][i] != want {
			t.Errorf("header column %d = %q, want %q", i, records[0][i], want)
		}
	}
	if got := records[1]; got[0] != "1" || got[1] != "501" || got[2] != "High" || got[3] != "0.940" {
		t.Errorf("first data row = %v, want [1 501 High 0.940]", got)
	}
}

func TestFilterByConfidence(t *testing.T) {
	tests := []struct {
		min  string
		want int
	}{
		{"Low", 3},
		{"Medium", 2},
		{"High", 1},
		{"medium", 2}, // case-insensitive
		{"", 3},       // unrecognized falls back to Low, the safest default
		{"nonsense", 3},
	}
	for _, tc := range tests {
		if got := len(filterByConfidence(sample(), tc.min)); got != tc.want {
			t.Errorf("filterByConfidence(%q) kept %d matches, want %d", tc.min, got, tc.want)
		}
	}
}

func TestWriteTableReportsCounts(t *testing.T) {
	var buf strings.Builder
	if err := writeTable(&buf, sample(), 1000); err != nil {
		t.Fatalf("writeTable: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"1000 contacts scanned", "3 candidate duplicate pairs", "High: 1", "Medium: 1", "Low: 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output is missing %q\ngot:\n%s", want, out)
		}
	}
}

func TestWriteExplanation(t *testing.T) {
	var buf strings.Builder
	if err := writeExplanation(&buf, sample(), "501"); err != nil {
		t.Fatalf("writeExplanation: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "1 <-> 501") || !strings.Contains(out, "email: identical") {
		t.Errorf("explanation is missing the pair or its reasoning:\n%s", out)
	}
	// Only the requested contact's matches are shown.
	if strings.Contains(out, "502") {
		t.Errorf("explanation leaked an unrelated contact:\n%s", out)
	}

	// A contact with no matches must say so rather than print nothing.
	buf.Reset()
	if err := writeExplanation(&buf, sample(), "9999"); err != nil {
		t.Fatalf("writeExplanation: %v", err)
	}
	if !strings.Contains(buf.String(), "no matches") {
		t.Errorf("expected a 'no matches' message, got %q", buf.String())
	}
}

func TestTitleCase(t *testing.T) {
	for in, want := range map[string]string{
		"high": "High", "HIGH": "High", " medium ": "Medium", "": "",
	} {
		if got := titleCase(in); got != want {
			t.Errorf("titleCase(%q) = %q, want %q", in, got, want)
		}
	}
}
