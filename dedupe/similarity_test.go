package dedupe

import (
	"math"
	"testing"
)

func TestJaroWinkler(t *testing.T) {
	tests := []struct {
		a, b    string
		wantMin float64
		wantMax float64
	}{
		{"ciara", "ciara", 1.0, 1.0},
		{"", "", 1.0, 1.0},
		{"ciara", "", 0.0, 0.0},
		{"cathy", "cathi", 0.85, 1.0},    // trailing typo: still clearly the same name
		{"martha", "marhta", 0.90, 1.0},  // transposition: the case Jaro exists for
		{"french", "franco", 0.70, 0.90}, // similar but distinct surnames
		{"victor", "freya", 0.0, 0.60},   // unrelated
	}
	for _, tc := range tests {
		got := jaroWinkler(tc.a, tc.b)
		if got < tc.wantMin || got > tc.wantMax {
			t.Errorf("jaroWinkler(%q, %q) = %.3f, want within [%.2f, %.2f]", tc.a, tc.b, got, tc.wantMin, tc.wantMax)
		}
	}
}

// TestJaroWinklerPrefixBonusIsGated: the prefix boost must not lift two
// unrelated strings just because they start with the same letter.
func TestJaroWinklerPrefixBonusIsGated(t *testing.T) {
	if got := jaroWinkler("carl", "christopher"); got > 0.75 {
		t.Errorf("jaroWinkler(carl, christopher) = %.3f, too high for unrelated names", got)
	}
}

func TestJaroWinklerIsSymmetric(t *testing.T) {
	pairs := [][2]string{{"ciara", "c"}, {"martha", "marhta"}, {"french", "franco"}, {"", "abc"}}
	for _, p := range pairs {
		ab, ba := jaroWinkler(p[0], p[1]), jaroWinkler(p[1], p[0])
		if math.Abs(ab-ba) > 1e-9 {
			t.Errorf("jaroWinkler(%q,%q)=%.6f but (%q,%q)=%.6f", p[0], p[1], ab, p[1], p[0], ba)
		}
	}
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"abc", "abc", 0},
		{"kitten", "sitting", 3}, // the textbook case
		{"flaw", "lawn", 2},
	}
	for _, tc := range tests {
		if got := levenshtein(tc.a, tc.b); got != tc.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestLevenshteinRatio(t *testing.T) {
	if got := levenshteinRatio("", ""); got != 1 {
		t.Errorf("levenshteinRatio of two empty strings = %.3f, want 1", got)
	}
	same := "449 6990 tellus road"
	if got := levenshteinRatio(same, same); got != 1 {
		t.Errorf("levenshteinRatio of identical strings = %.3f, want 1", got)
	}
	// A dropped apartment prefix should still read as the same street address.
	if got := levenshteinRatio("apartment 312 8611 lacus avenue", "312 8611 lacus avenue"); got < 0.65 {
		t.Errorf("levenshteinRatio for a dropped unit prefix = %.3f, unexpectedly low", got)
	}
	// Two different streets should not.
	if got := levenshteinRatio("4811 aliquam street", "735 3498 magna street"); got > 0.6 {
		t.Errorf("levenshteinRatio for different streets = %.3f, unexpectedly high", got)
	}
}
