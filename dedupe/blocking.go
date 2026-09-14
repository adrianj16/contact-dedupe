package dedupe

import (
	"sort"
	"strings"
)

// Blocking. Comparing every contact against every other is O(n^2): 499,500
// comparisons for the 1,000-row sample, and 50 billion for a 100k-row list.
// Almost all of those pairs share nothing at all.
//
// Instead, each contact is filed under several cheap equality keys. Only
// contacts that land in the same bucket under at least one key are scored.
// Using several independent keys is what keeps recall high: a pair whose email
// was rewritten is still caught by its address key, and a pair that moved
// house is still caught by its email key.

// blockKeys returns the keys a contact is filed under. An empty key means the
// underlying field was blank, and is skipped — blank must never act as a
// join value, or every incomplete row would collide with every other.
func blockKeys(c Contact) []string {
	n := c.norm
	var keys []string
	seen := make(map[string]bool)
	add := func(prefix, val string) {
		if val == "" {
			return
		}
		// A contact must be filed under each key at most once. The two name
		// keys collapse to the same string when both names are already
		// initials ("C F"), and a repeated key would otherwise pair the
		// contact with itself.
		k := prefix + ":" + val
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}

	// The mailbox name alone, so a changed provider does not hide the pair.
	add("email", n.emailLocal)
	add("addr", n.address)

	// Name keys are built from an initial plus a whole name, which is exactly
	// the shape that survives this dataset's abbreviation: "Ciara French" and
	// "C French" both produce "name:c|french". Both orders are emitted since
	// either name may be the abbreviated one.
	if n.last != "" && n.first != "" {
		add("name", firstRune(n.first)+"|"+n.last)
		add("name", n.first+"|"+firstRune(n.last))
	}

	// ZIP is a weak key on its own, so it is paired with a last-name initial
	// to stay selective while still catching rows that lost their email.
	if n.zip != "" && n.last != "" {
		add("zip", n.zip+"|"+firstRune(n.last))
	}

	// Distinctive street-name tokens. The whole-address key above is exact, so
	// it misses a record whose house number was dropped ("668-4116 Maecenas
	// St." vs "Maecenas street."). Indexing the street name itself still
	// brings that pair together. Generic street-type words are excluded
	// because "street" is shared by hundreds of rows and identifies nobody.
	for _, tok := range strings.Fields(n.address) {
		if len(tok) >= 4 && !genericAddressWord[tok] {
			add("addrtok", tok)
		}
	}

	// Both initials. This is the last resort for the hardest rows in the
	// input: those where the first name, the last name, the email and the ZIP
	// were all reduced to initials or blanks. It is a deliberately weak key —
	// 676 buckets for the whole alphabet — and it is safe only because
	// MaxBlockSize refuses to expand a bucket that grows unselective.
	if n.first != "" && n.last != "" {
		add("initials", firstRune(n.first)+"|"+firstRune(n.last))
	}
	return keys
}

// genericAddressWord lists the tokens that describe the *kind* of a street
// rather than which street it is. They are useless as blocking keys.
var genericAddressWord = map[string]bool{
	"street": true, "road": true, "avenue": true, "drive": true, "lane": true,
	"boulevard": true, "court": true, "place": true, "highway": true,
	"parkway": true, "circle": true, "apartment": true, "suite": true,
	"box": true, "north": true, "south": true, "east": true, "west": true,
	"northeast": true, "northwest": true, "southeast": true, "southwest": true,
}

func firstRune(s string) string {
	if s == "" {
		return ""
	}
	return string([]rune(s)[0])
}

// candidatePairs builds the blocking index and returns the deduplicated set of
// index pairs worth scoring, as [smaller, larger] slice positions.
func candidatePairs(contacts []Contact, maxBlockSize int) [][2]int {
	if maxBlockSize <= 0 {
		maxBlockSize = DefaultOptions.MaxBlockSize
	}

	index := make(map[string][]int, len(contacts)*2)
	for i, c := range contacts {
		for _, k := range blockKeys(c) {
			index[k] = append(index[k], i)
		}
	}

	seen := make(map[[2]int]struct{})
	for _, members := range index {
		// A key held by one contact yields no pair; a key held by hundreds is
		// not selective enough to be worth expanding.
		if len(members) < 2 || len(members) > maxBlockSize {
			continue
		}
		for i := 0; i < len(members); i++ {
			for j := i + 1; j < len(members); j++ {
				a, b := members[i], members[j]
				if a == b {
					continue // defensive: never pair a contact with itself
				}
				if a > b {
					a, b = b, a
				}
				seen[[2]int{a, b}] = struct{}{}
			}
		}
	}

	pairs := make([][2]int, 0, len(seen))
	for p := range seen {
		pairs = append(pairs, p)
	}
	// Map iteration is random; sort so that a run is byte-for-byte repeatable.
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i][0] != pairs[j][0] {
			return pairs[i][0] < pairs[j][0]
		}
		return pairs[i][1] < pairs[j][1]
	})
	return pairs
}
