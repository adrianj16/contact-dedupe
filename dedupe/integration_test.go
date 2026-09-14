package dedupe

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// sampleFile is the input supplied with the assessment packet. Tests that use
// it skip rather than fail when it is absent, so the unit tests still run in
// isolation.
const sampleFile = "../input_sample.csv"

func loadSample(t *testing.T) []Contact {
	t.Helper()
	f, err := os.Open(sampleFile)
	if err != nil {
		t.Skipf("sample input not available: %v", err)
	}
	defer f.Close()

	contacts, err := LoadCSV(f)
	if err != nil {
		t.Fatalf("loading %s: %v", sampleFile, err)
	}
	return contacts
}

func TestLoadSampleFile(t *testing.T) {
	contacts := loadSample(t)
	if len(contacts) != 1000 {
		t.Errorf("loaded %d contacts, want 1000", len(contacts))
	}
	for _, c := range contacts {
		if c.ID == "" {
			t.Fatal("loaded a contact with an empty ID")
		}
	}
}

func TestLoadCSVRejectsBadHeader(t *testing.T) {
	_, err := LoadCSV(stringReader("id,first,last\n1,a,b\n"))
	if err == nil {
		t.Error("expected an error for an unrecognized header, got nil")
	}
}

func TestLoadCSVHandlesQuotedFields(t *testing.T) {
	const in = "contactID,name,name1,email,postalZip,address\n" +
		"3,Victor,Savage,orci@protonmail.net,82025,\"P.O. Box 775, 8910 Arcu. Road\"\n"
	contacts, err := LoadCSV(stringReader(in))
	if err != nil {
		t.Fatalf("LoadCSV: %v", err)
	}
	if want := "P.O. Box 775, 8910 Arcu. Road"; contacts[0].Address != want {
		t.Errorf("address = %q, want %q (embedded comma must survive)", contacts[0].Address, want)
	}
}

// TestOnSampleFile measures the matcher against the full input.
//
// The file carries a usable ground truth: it is 500 contacts followed by a
// deliberately degraded copy of each, so contact N and contact N+500 are the
// same person. That is a property of this particular file, not an assumption
// the matcher makes — no part of the algorithm knows about it — but it lets
// the solution be reported with real precision and recall instead of a
// spot-check. See README.md for the numbers and the discussion.
func TestOnSampleFile(t *testing.T) {
	contacts := loadSample(t)
	matches := FindDuplicates(contacts, DefaultOptions)

	const seededPairs = 500
	truth := func(a, b string) bool {
		na, errA := strconv.Atoi(a)
		nb, errB := strconv.Atoi(b)
		if errA != nil || errB != nil {
			return false
		}
		if na > nb {
			na, nb = nb, na
		}
		return nb-na == seededPairs && na <= seededPairs
	}

	var truePos, falsePos int
	found := make(map[int]bool)
	byConfidence := map[Confidence][2]int{} // confidence -> [correct, total]
	for _, m := range matches {
		correct := truth(m.SourceID, m.MatchID)
		c := byConfidence[m.Confidence]
		c[1]++
		if correct {
			c[0]++
			truePos++
			n, _ := strconv.Atoi(m.SourceID)
			found[n] = true
		} else {
			falsePos++
		}
		byConfidence[m.Confidence] = c
	}

	precision := float64(truePos) / float64(len(matches))
	recall := float64(len(found)) / float64(seededPairs)

	// Precision over the actionable bands. Low is a review queue by design
	// (see the discussion in README.md and DefaultOptions), so a caller acting
	// on results automatically would filter to Medium and above, and that is
	// the number worth defending.
	var actionableCorrect, actionableTotal int
	for _, c := range []Confidence{High, Medium} {
		actionableCorrect += byConfidence[c][0]
		actionableTotal += byConfidence[c][1]
	}
	actionable := float64(actionableCorrect) / float64(actionableTotal)

	t.Logf("pairs reported: %d  true: %d  false: %d", len(matches), truePos, falsePos)
	t.Logf("recall: %.1f%%  precision (all bands): %.1f%%  precision (Medium+): %.1f%%",
		100*recall, 100*precision, 100*actionable)
	for _, c := range []Confidence{High, Medium, Low} {
		if v := byConfidence[c]; v[1] > 0 {
			t.Logf("  %-6s %4d reported, %4d correct (%.1f%% precision)", c, v[1], v[0], 100*float64(v[0])/float64(v[1]))
		}
	}

	// Regression floors, set below the measured results so an accidental
	// change in scoring is caught without the test being brittle.
	if recall < 0.98 {
		t.Errorf("recall %.1f%% is below the 98%% floor", 100*recall)
	}
	if actionable < 0.98 {
		t.Errorf("Medium-and-above precision %.1f%% is below the 98%% floor", 100*actionable)
	}

	// High must be trustworthy above all: it is the label a caller would
	// auto-merge on without human review.
	if v := byConfidence[High]; v[1] > 0 {
		if p := float64(v[0]) / float64(v[1]); p < 0.99 {
			t.Errorf("High-accuracy precision %.1f%% is below the 99%% floor", 100*p)
		}
	}
	if v := byConfidence[High]; v[1] < 300 {
		t.Errorf("only %d pairs reached High; the confident band should carry most of the 500 seeded duplicates", v[1])
	}
}

// BenchmarkFindDuplicates records the cost of a full run over the sample.
func BenchmarkFindDuplicates(b *testing.B) {
	f, err := os.Open(sampleFile)
	if err != nil {
		b.Skip("sample input not available")
	}
	contacts, err := LoadCSV(f)
	f.Close()
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		FindDuplicates(contacts, DefaultOptions)
	}
}

func stringReader(s string) *strings.Reader { return strings.NewReader(s) }
