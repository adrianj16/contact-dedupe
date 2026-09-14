package dedupe

import (
	"strings"
	"testing"
)

// TestBlockKeysSkipBlanks: a blank field must never become a join value, or
// every incomplete row would be a candidate against every other one.
func TestBlockKeysSkipBlanks(t *testing.T) {
	c := contact("1", "", "", "", "", "")
	if got := blockKeys(c); len(got) != 0 {
		t.Errorf("a fully blank contact produced keys %v, want none", got)
	}

	partial := contact("2", "Ciara", "French", "", "", "")
	for _, k := range blockKeys(partial) {
		if strings.HasSuffix(k, ":") || strings.Contains(k, "|:") {
			t.Errorf("key %q was built from a blank value", k)
		}
	}
}

// TestBlockKeysAreUnique guards the self-pairing bug: when both names are
// already initials, the two name keys collapse to the same string, and a
// repeated key would pair the contact with itself.
func TestBlockKeysAreUnique(t *testing.T) {
	c := contact("1001", "C", "F", "mollis@outlook.net", "39746", "449-6990 Tellus Rd.")
	seen := map[string]bool{}
	for _, k := range blockKeys(c) {
		if seen[k] {
			t.Errorf("duplicate blocking key %q", k)
		}
		seen[k] = true
	}
}

func TestNoSelfPairs(t *testing.T) {
	contacts := []Contact{
		contact("1001", "C", "F", "mollis@outlook.net", "39746", "449-6990 Tellus Rd."),
		contact("1002", "C", "F", "mollis@outlook.net", "39746", "449-6990 Tellus Rd."),
	}
	for _, p := range candidatePairs(contacts, DefaultOptions.MaxBlockSize) {
		if p[0] == p[1] {
			t.Errorf("contact at index %d was paired with itself", p[0])
		}
	}
	for _, m := range FindDuplicates(contacts, DefaultOptions) {
		if m.SourceID == m.MatchID {
			t.Errorf("contact %s was reported as a duplicate of itself", m.SourceID)
		}
	}
}

// TestBlockingFindsPairsThroughAnyKey is the reason several independent keys
// exist: each pair below agrees on a different field, and each must survive
// blocking even though the others would not find it.
func TestBlockingFindsPairsThroughAnyKey(t *testing.T) {
	tests := []struct {
		name string
		a, b Contact
	}{
		{
			"found via email when the address changed",
			contact("1", "Ciara", "French", "cf@outlook.net", "39746", "449-6990 Tellus Rd."),
			contact("2", "C", "French", "cf@outlook.net", "11111", "1 Somewhere Else"),
		},
		{
			"found via address when the email changed",
			contact("3", "Ciara", "French", "cf@outlook.net", "39746", "449-6990 Tellus Rd."),
			contact("4", "C", "French", "ciara.f@zoho.ca", "39746", "449-6990 Tellus Rd."),
		},
		{
			"found via name when only names are recorded on one side",
			contact("5", "Ciara", "French", "cf@outlook.net", "39746", "449-6990 Tellus Rd."),
			contact("6", "C", "French", "", "39746", ""),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			contacts := []Contact{tc.a, tc.b}
			if got := candidatePairs(contacts, DefaultOptions.MaxBlockSize); len(got) != 1 {
				t.Fatalf("blocking produced %d candidate pairs, want 1", len(got))
			}
		})
	}
}

// TestBlockingCutsTheSearchSpace: blocking is the reason this scales. On the
// real input it must consider far fewer pairs than the n(n-1)/2 of a full
// cross join, while still finding the duplicates (verified in TestOnSampleFile).
func TestBlockingCutsTheSearchSpace(t *testing.T) {
	contacts := loadSample(t)

	n := len(contacts)
	bruteForce := n * (n - 1) / 2
	candidates := len(candidatePairs(contacts, DefaultOptions.MaxBlockSize))

	if candidates >= bruteForce/10 {
		t.Errorf("blocking kept %d of %d possible pairs; expected an order-of-magnitude reduction",
			candidates, bruteForce)
	}
	t.Logf("blocking: %d candidate pairs vs %d for a full cross join (%.1f%%)",
		candidates, bruteForce, 100*float64(candidates)/float64(bruteForce))
}

// TestMaxBlockSizeIsHonoured: an unselective key must not be expanded into a
// quadratic number of pairs.
func TestMaxBlockSizeIsHonoured(t *testing.T) {
	// 50 contacts that agree only on a shared ZIP and surname initial.
	var contacts []Contact
	for i := 0; i < 50; i++ {
		contacts = append(contacts, contact(string(rune('a'+i%26))+itoa(i), "Person"+itoa(i), "Smith", "", "99999", ""))
	}
	if got := candidatePairs(contacts, 10); len(got) != 0 {
		t.Errorf("a 50-member block was expanded into %d pairs despite MaxBlockSize=10", len(got))
	}
	if got := candidatePairs(contacts, 100); len(got) == 0 {
		t.Error("the same block produced no pairs at MaxBlockSize=100")
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
