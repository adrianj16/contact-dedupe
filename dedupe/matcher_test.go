package dedupe

import (
	"math"
	"strings"
	"testing"
)

// contact is a test helper that builds a normalized Contact from raw fields,
// so tests read as the CSV row they represent.
func contact(id, first, last, email, zip, address string) Contact {
	c := Contact{ID: id, FirstName: first, LastName: last, Email: email, Zip: zip, Address: address}
	c.norm = normalize(c)
	return c
}

// TestAssessmentExample is the acceptance test for this solution: it is the
// worked example from the assessment brief, and every pair the brief lists
// must be reported with the accuracy the brief gives it.
//
// The brief's table is read as the matches *for contact 1001* — it is
// introduced as an illustration of "a contact might have multiple matches",
// and both of its rows have 1001 as the source. Extra pairs among the other
// contacts are therefore allowed, and one does occur: 1002<->1003 agree on a
// first initial, a last initial and a ZIP, which is the same weak-evidence
// shape the brief itself labels Low for 1001<->1003. See TestKnownWeakPair.
func TestAssessmentExample(t *testing.T) {
	contacts := []Contact{
		contact("1001", "C", "F", "mollis.lectus.pede@outlook.net", "", "449-6990 Tellus. Rd."),
		contact("1002", "C", "French", "mollis.lectus.pede@outlook.net", "39746", "449-6990 Tellus. Rd."),
		contact("1003", "Ciara", "F", "non.lacinia.at@zoho.ca", "39746", ""),
	}

	got := map[[2]string]Match{}
	for _, m := range FindDuplicates(contacts, DefaultOptions) {
		got[[2]string{m.SourceID, m.MatchID}] = m
	}

	want := map[[2]string]Confidence{
		{"1001", "1002"}: High,
		{"1001", "1003"}: Low,
	}
	for key, expected := range want {
		m, ok := got[key]
		if !ok {
			t.Errorf("missing expected match %s<->%s", key[0], key[1])
			continue
		}
		if m.Confidence != expected {
			t.Errorf("%s<->%s: accuracy = %s (score %.3f), want %s\nreasons: %s",
				key[0], key[1], m.Confidence, m.Score, expected, strings.Join(m.Reasons, "; "))
		}
	}
}

// TestKnownWeakPair pins down the limit of this scoring model, so the trade-off
// is explicit rather than accidental.
//
// The brief requires 1001<->1003 — two consistent initials, a disagreeing
// email, nothing else — to be reported as Low. That is the weakest evidence
// pattern the data contains, and no rule can surface it while suppressing the
// hundreds of unrelated pairs that share exactly that shape. The reporting
// floor is therefore set to admit it, and Low is documented as a low-precision
// review queue rather than a claim of duplication.
func TestKnownWeakPair(t *testing.T) {
	a := contact("1001", "C", "F", "mollis.lectus.pede@outlook.net", "", "449-6990 Tellus. Rd.")
	b := contact("1003", "Ciara", "F", "non.lacinia.at@zoho.ca", "39746", "")

	m, ok := scorePair(a, b, DefaultOptions)
	if !ok {
		t.Fatal("the brief's weak pair was not reported at all")
	}
	if m.Confidence != Low {
		t.Errorf("accuracy = %s (%.3f), want Low — two initials are not a confident match", m.Confidence, m.Score)
	}
	// An unrelated pair with the same shape must land in the same band: the
	// model must not be tuned to recognize this specific example.
	c := contact("9001", "Sybill", "Boyer", "eu.odio@hotmail.couk", "77937", "")
	d := contact("9002", "S", "B", "quis.diam@aol.couk", "77937", "")
	other, ok := scorePair(c, d, DefaultOptions)
	if !ok || other.Confidence != Low {
		t.Errorf("an equivalently weak unrelated pair was treated differently (ok=%v, %s)", ok, other.Confidence)
	}
}

func TestScoreAndConfidence(t *testing.T) {
	tests := []struct {
		name    string
		a, b    Contact
		want    Confidence
		noMatch bool
	}{
		{
			name: "identical rows are High",
			a:    contact("1", "Ciara", "French", "m.l.p@outlook.net", "39746", "449-6990 Tellus Rd."),
			b:    contact("2", "Ciara", "French", "m.l.p@outlook.net", "39746", "449-6990 Tellus Rd."),
			want: High,
		},
		{
			// The dominant pattern in the sample data.
			name: "first name abbreviated to an initial, everything else equal",
			a:    contact("1", "Hilary", "Franco", "congue.in@icloud.com", "25211", "331-981 Velit. Road"),
			b:    contact("2", "H", "Franco", "congue.in@icloud.com", "25211", "331-981 Velit. Road"),
			want: High,
		},
		{
			// The second seeded pattern: same mailbox, rewritten provider.
			name: "email domain changed but everything else equal",
			a:    contact("1", "Charles", "Pacheco", "nulla.eget@protonmail.couk", "76837", "Ap #312-8611 Lacus. Ave"),
			b:    contact("2", "C", "Pacheco", "nulla.eget@att.couk", "76837", "Ap #312-8611 Lacus. Ave"),
			want: High,
		},
		{
			// The classic false positive a naive surname+address rule produces:
			// two members of one household. The first names contradict and no
			// shared mailbox overrides them, so the pair is vetoed.
			name:    "same household, different people are not duplicates",
			a:       contact("1", "John", "Smith", "john.smith@mail.com", "12345", "12 Oak Street"),
			b:       contact("2", "Mary", "Smith", "mary.smith@mail.com", "12345", "12 Oak Street"),
			noMatch: true,
		},
		{
			// ...but a shared mailbox overrides a disagreeing name, which is
			// what lets a nickname or a changed surname still match.
			name: "shared mailbox overrides a differing first name",
			a:    contact("1", "Robert", "Smith", "rsmith@mail.com", "12345", "12 Oak Street"),
			b:    contact("2", "Bob", "Smith", "rsmith@mail.com", "12345", "12 Oak Street"),
			want: High,
		},
		{
			name:    "unrelated contacts do not match",
			a:       contact("1", "Victor", "Savage", "orci@protonmail.net", "82025", "P.O. Box 775, 8910 Arcu. Road"),
			b:       contact("2", "Freya", "West", "vivamus.rhoncus@aol.com", "83604", "4811 Aliquam St."),
			noMatch: true,
		},
		{
			name:    "disagreeing initials block the match",
			a:       contact("1", "A", "Smith", "", "12345", ""),
			b:       contact("2", "B", "Smith", "", "12345", ""),
			noMatch: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := scorePair(tc.a, tc.b, DefaultOptions)
			if tc.noMatch {
				if ok {
					t.Fatalf("expected no match, got %s (%.3f): %s", got.Confidence, got.Score, strings.Join(got.Reasons, "; "))
				}
				return
			}
			if !ok {
				t.Fatal("expected a match, got none")
			}
			if got.Confidence != tc.want {
				t.Errorf("accuracy = %s (%.3f), want %s\nreasons: %s", got.Confidence, got.Score, tc.want, strings.Join(got.Reasons, "; "))
			}
		})
	}
}

// TestMissingFieldsDoNotPenalize is the property that makes the weighted-
// average-over-available-weight design worth having: an incomplete row that
// agrees on everything it does have must not score lower than a complete one.
func TestMissingFieldsDoNotPenalize(t *testing.T) {
	complete := []Contact{
		contact("1", "Ciara", "French", "clp@outlook.net", "39746", "449-6990 Tellus Rd."),
		contact("2", "C", "French", "clp@outlook.net", "39746", "449-6990 Tellus Rd."),
	}
	sparse := []Contact{
		contact("3", "Ciara", "French", "clp@outlook.net", "", ""),
		contact("4", "C", "French", "clp@outlook.net", "", ""),
	}

	full, ok := scorePair(complete[0], complete[1], DefaultOptions)
	if !ok {
		t.Fatal("complete pair did not match")
	}
	partial, ok := scorePair(sparse[0], sparse[1], DefaultOptions)
	if !ok {
		t.Fatal("sparse pair did not match")
	}
	if partial.Score < full.Score-0.05 {
		t.Errorf("sparse pair scored %.3f, materially below the complete pair's %.3f", partial.Score, full.Score)
	}
	if partial.Confidence != High {
		t.Errorf("sparse pair accuracy = %s, want High", partial.Confidence)
	}
}

// TestBlankFieldsAreNeverEvidence guards the most dangerous failure mode in
// this dataset: two rows that are blank in the same place must not be treated
// as agreeing there. Without this, the ~50 rows missing an email would all
// "match" each other.
func TestBlankFieldsAreNeverEvidence(t *testing.T) {
	a := contact("1", "Alice", "Anderson", "", "", "")
	b := contact("2", "Bob", "Brown", "", "", "")

	if _, ok := scorePair(a, b, DefaultOptions); ok {
		t.Error("two rows blank in the same fields were reported as a match")
	}

	// And the field comparators must each report "not comparable" on blanks.
	if compareEmail(a.norm, b.norm).comparable {
		t.Error("compareEmail treated two blank emails as comparable")
	}
	if compareZip("", "").comparable {
		t.Error("compareZip treated two blank ZIPs as comparable")
	}
	if compareAddress("", "").comparable {
		t.Error("compareAddress treated two blank addresses as comparable")
	}
}

// TestNamesAloneAreNotEvidence covers the common-name guard. Two "John Smith"
// rows with no email, address or ZIP between them score 1.0 on every field
// that can be compared, and must still not be reported: a shared common name
// says two people have similar names, not that they are one person.
func TestNamesAloneAreNotEvidence(t *testing.T) {
	cases := []struct {
		name string
		a, b Contact
	}{
		{
			"identical common names, nothing else recorded",
			contact("1", "John", "Smith", "", "", ""),
			contact("2", "John", "Smith", "", "", ""),
		},
		{
			"consistent initials, nothing else recorded",
			contact("3", "R", "M", "", "", ""),
			contact("4", "Russell", "Morris", "", "", ""),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if m, ok := scorePair(tc.a, tc.b, DefaultOptions); ok {
				t.Errorf("reported as %s (%.3f); names alone are not evidence", m.Confidence, m.Score)
			}
		})
	}

	// One contact detail present on both sides is enough to make the pair
	// reportable — even a disagreeing one, since that still means the records
	// are comparable on something beyond a name.
	a := contact("5", "John", "Smith", "", "12345", "")
	b := contact("6", "John", "Smith", "", "12345", "")
	if _, ok := scorePair(a, b, DefaultOptions); !ok {
		t.Error("a name match corroborated by a matching ZIP should be reported")
	}
}

// TestMultipleAndNoMatches covers requirement 3 of the brief directly.
func TestMultipleAndNoMatches(t *testing.T) {
	contacts := []Contact{
		contact("1", "Ciara", "French", "cf@outlook.net", "39746", "449-6990 Tellus Rd."),
		contact("2", "C", "French", "cf@outlook.net", "39746", "449-6990 Tellus Rd."),
		contact("3", "Ciara", "F", "cf@outlook.net", "39746", "449-6990 Tellus Rd."),
		contact("4", "Zachary", "Quinn", "zq@example.org", "10001", "1 Nowhere Lane"),
	}

	matches := FindDuplicates(contacts, DefaultOptions)

	partners := map[string]int{}
	for _, m := range matches {
		partners[m.SourceID]++
		partners[m.MatchID]++
	}
	if partners["1"] != 2 {
		t.Errorf("contact 1 has %d matches, want 2 (a contact may have several)", partners["1"])
	}
	if partners["4"] != 0 {
		t.Errorf("contact 4 has %d matches, want 0 (a contact may have none)", partners["4"])
	}
}

// TestPairsReportedOnce checks that a pair is emitted in one direction only,
// with the lower ID as the source, matching the brief's output format.
func TestPairsReportedOnce(t *testing.T) {
	contacts := []Contact{
		contact("20", "Ciara", "French", "cf@outlook.net", "39746", "449-6990 Tellus Rd."),
		contact("3", "C", "French", "cf@outlook.net", "39746", "449-6990 Tellus Rd."),
	}
	matches := FindDuplicates(contacts, DefaultOptions)
	if len(matches) != 1 {
		t.Fatalf("got %d matches, want 1 (each pair reported once)", len(matches))
	}
	if matches[0].SourceID != "3" || matches[0].MatchID != "20" {
		t.Errorf("got %s -> %s, want 3 -> 20 (lower ID is the source, compared numerically)",
			matches[0].SourceID, matches[0].MatchID)
	}
}

// TestDeterministic guards against map-iteration order leaking into results.
func TestDeterministic(t *testing.T) {
	contacts := []Contact{
		contact("1", "Ciara", "French", "cf@outlook.net", "39746", "449-6990 Tellus Rd."),
		contact("2", "C", "French", "cf@outlook.net", "39746", "449-6990 Tellus Rd."),
		contact("3", "Hilary", "Franco", "hf@icloud.com", "25211", "331-981 Velit Road"),
		contact("4", "H", "Franco", "hf@icloud.com", "25211", "331-981 Velit Road"),
	}
	first := FindDuplicates(contacts, DefaultOptions)
	for i := 0; i < 20; i++ {
		got := FindDuplicates(contacts, DefaultOptions)
		if len(got) != len(first) {
			t.Fatalf("run %d returned %d matches, first run returned %d", i, len(got), len(first))
		}
		for j := range got {
			if got[j].SourceID != first[j].SourceID || got[j].MatchID != first[j].MatchID ||
				math.Abs(got[j].Score-first[j].Score) > 1e-9 {
				t.Fatalf("run %d differs at position %d: %v vs %v", i, j, got[j], first[j])
			}
		}
	}
}

// TestScoreIsSymmetric: comparing a to b must equal comparing b to a.
func TestScoreIsSymmetric(t *testing.T) {
	a := contact("1", "Ciara", "French", "cf@outlook.net", "39746", "449-6990 Tellus Rd.")
	b := contact("2", "C", "French", "cf@zoho.ca", "", "449-6990 Tellus Rd.")

	ab, okAB := scorePair(a, b, DefaultOptions)
	ba, okBA := scorePair(b, a, DefaultOptions)
	if okAB != okBA {
		t.Fatalf("match decision is not symmetric: a->b %v, b->a %v", okAB, okBA)
	}
	if math.Abs(ab.Score-ba.Score) > 1e-9 {
		t.Errorf("score is not symmetric: a->b %.6f, b->a %.6f", ab.Score, ba.Score)
	}
}

func TestEmptyAndSingletonInput(t *testing.T) {
	if got := FindDuplicates(nil, DefaultOptions); len(got) != 0 {
		t.Errorf("nil input produced %d matches, want 0", len(got))
	}
	one := []Contact{contact("1", "Ciara", "French", "cf@outlook.net", "39746", "449 Tellus Rd.")}
	if got := FindDuplicates(one, DefaultOptions); len(got) != 0 {
		t.Errorf("single contact produced %d matches, want 0", len(got))
	}
}
