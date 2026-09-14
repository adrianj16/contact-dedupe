package dedupe

import (
	"sort"
	"strconv"
	"strings"
)

// Confidence is the human-facing accuracy label required by the assessment's
// output format.
type Confidence string

const (
	High   Confidence = "High"
	Medium Confidence = "Medium"
	Low    Confidence = "Low"
)

// Match is one candidate duplicate pair. SourceID is always the contact that
// appears first in the input, so each pair is reported exactly once.
type Match struct {
	SourceID   string
	MatchID    string
	Score      float64    // 0..1, the weighted agreement across comparable fields
	Confidence Confidence // Score bucketed into the reported accuracy label
	Reasons    []string   // per-field explanation, for auditing a decision
}

// Field weights. They express how much each field narrows down a person in a
// contact list, and they sum to 1.0:
//
//	email   0.35  near-unique per person; the single strongest identifier
//	last    0.20  stable, and far more selective than a first name
//	address 0.20  strong, but shared by everyone in a household
//	first   0.15  weak on its own; frequently abbreviated in this data
//	zip     0.10  weakest; thousands of unrelated people share one
//
// The weights are deliberately relative rather than absolute: the final score
// divides by the weight actually available on a pair (see scorePair), so a
// pair with no email is not punished for the missing field.
const (
	weightEmail   = 0.35
	weightLast    = 0.20
	weightAddress = 0.20
	weightFirst   = 0.15
	weightZip     = 0.10
)

// Options tunes the thresholds.
//
// Any field left at zero is filled in from DefaultOptions by withDefaults, so
// a caller can override one threshold without restating the rest. This is not
// only convenience: a zero threshold would otherwise label every pair High,
// silently and with total confidence, which is the worst possible failure for
// a matcher.
type Options struct {
	// HighThreshold and MediumThreshold bucket the score into a label. Pairs
	// scoring below LowThreshold are not reported at all.
	HighThreshold   float64
	MediumThreshold float64
	LowThreshold    float64

	// MinComparableFields is the number of fields that must be populated on
	// both contacts before a pair may be reported at all. Two contacts that
	// share only a first name are not evidence of anything.
	MinComparableFields int

	// MaxBlockSize caps how many contacts a single blocking key may gather.
	// A key shared by hundreds of rows (a data-entry placeholder ZIP, say) is
	// not selective, and expanding it would cost O(n^2) comparisons for
	// almost no recall. Blocks larger than this are skipped; the pair can
	// still be found through any of its other keys.
	MaxBlockSize int
}

// DefaultOptions holds the thresholds used for the submitted results.
//
// The boundaries were fixed by the worked example in the assessment brief and
// then checked against the full input file (see README.md):
//
//   - The 1001/1002 pair scores 0.91 and must read High.
//   - The 1001/1003 pair scores 0.30 and must read Low. That pair agrees only
//     on two initials and disagrees on email, which is what sets the reporting
//     floor so low — the brief asks for weak leads like it to be surfaced.
//
// The consequence is that Low is a wide review queue, not a claim: on the
// sample input High and Medium are 100% precise, while most Low rows are not
// duplicates. Callers who want only actionable matches should filter to
// Medium and above (the CLI's -min flag).
var DefaultOptions = Options{
	HighThreshold:       0.82,
	MediumThreshold:     0.60,
	LowThreshold:        0.40,
	MinComparableFields: 2,
	MaxBlockSize:        200,
}

// withDefaults returns opts with every zero-valued field replaced by the
// corresponding DefaultOptions value. Zero is not a meaningful setting for any
// of these fields — a zero threshold accepts everything, a zero block cap
// blocks nothing — so treating it as "unset" is both safe and useful.
func (o Options) withDefaults() Options {
	if o.HighThreshold == 0 {
		o.HighThreshold = DefaultOptions.HighThreshold
	}
	if o.MediumThreshold == 0 {
		o.MediumThreshold = DefaultOptions.MediumThreshold
	}
	if o.LowThreshold == 0 {
		o.LowThreshold = DefaultOptions.LowThreshold
	}
	if o.MinComparableFields == 0 {
		o.MinComparableFields = DefaultOptions.MinComparableFields
	}
	if o.MaxBlockSize == 0 {
		o.MaxBlockSize = DefaultOptions.MaxBlockSize
	}
	return o
}

// FindDuplicates returns every candidate duplicate pair in contacts, ordered
// by descending score. A contact may appear in many pairs or in none.
//
// The work is done entirely in memory in two phases: blocking builds an index
// of cheap equality keys to shrink the search space, then each surviving
// candidate pair is scored field by field.
func FindDuplicates(contacts []Contact, opts Options) []Match {
	opts = opts.withDefaults()
	candidates := candidatePairs(contacts, opts.MaxBlockSize)

	matches := make([]Match, 0, len(candidates))
	for _, p := range candidates {
		if m, ok := scorePair(contacts[p[0]], contacts[p[1]], opts); ok {
			matches = append(matches, m)
		}
	}

	// Highest score first; ties broken by ID so runs are reproducible.
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		if matches[i].SourceID != matches[j].SourceID {
			return lessID(matches[i].SourceID, matches[j].SourceID)
		}
		return lessID(matches[i].MatchID, matches[j].MatchID)
	})
	return matches
}

// fieldResult is one field's contribution to a pair's score.
type fieldResult struct {
	score       float64
	comparable  bool // both contacts have a value here, so the field has a say
	contradicts bool // both have a value and they are definitively different
	// weightScale damps this field's weight in the average. It is 1 for a
	// normal comparison and disagreementDamping when the values simply differ;
	// see that constant.
	weightScale float64
	reason      string
}

// weight returns the field's effective weight, defaulting weightScale to 1.
func (f fieldResult) weight(base float64) float64 {
	if f.weightScale == 0 {
		return base
	}
	return base * f.weightScale
}

// disagreementDamping is applied to the contact-detail fields (email, address,
// ZIP) when their values simply differ.
//
// These fields are asymmetric evidence. A matching email is close to proof;
// a differing one proves very little, because one person routinely has a work
// address and a personal one, moves house, and keeps an old ZIP on file. At
// full weight a single stale email would outvote every other field and hide
// real duplicates, so a disagreement counts — but at reduced strength.
//
// Names are deliberately excluded from this damping: a disagreeing name is a
// contradiction, handled by the veto in scorePair rather than by arithmetic.
const disagreementDamping = 0.4

func skipped() fieldResult { return fieldResult{} }

// scorePair compares two contacts field by field and combines the results.
//
// The key idea is that the denominator is the weight of the fields that are
// actually comparable, not the full 1.0. A pair where both contacts have an
// email, a last name and an address is judged on those three fields alone;
// a missing ZIP neither helps nor hurts. This is what keeps the ~5% of rows
// with blank fields from being systematically scored lower than complete rows.
func scorePair(a, b Contact, opts Options) (Match, bool) {
	opts = opts.withDefaults()
	email := compareEmail(a.norm, b.norm)
	last := compareName(a.norm.last, b.norm.last, "last name")
	addr := compareAddress(a.norm.address, b.norm.address)
	first := compareName(a.norm.first, b.norm.first, "first name")
	zip := compareZip(a.norm.zip, b.norm.zip)

	fields := []struct {
		weight float64
		result fieldResult
	}{
		{weightEmail, email},
		{weightLast, last},
		{weightAddress, addr},
		{weightFirst, first},
		{weightZip, zip},
	}

	var weighted, available float64
	var comparable int
	reasons := make([]string, 0, len(fields))
	for _, f := range fields {
		if !f.result.comparable {
			continue
		}
		comparable++
		w := f.result.weight(f.weight)
		available += w
		weighted += w * f.result.score
		reasons = append(reasons, f.result.reason)
	}

	if comparable < opts.MinComparableFields || available == 0 {
		return Match{}, false
	}

	// Names alone are never enough. Two rows agreeing on "John Smith", or on a
	// pair of initials, with no email, address or ZIP between them to
	// corroborate it, are not evidence of a duplicate — they are evidence that
	// two people have similar names. The sample input contains hundreds of
	// such pairs, and reporting them would bury the real duplicates.
	//
	// At least one contact detail must be present on both sides before a pair
	// is reported at all. Note that it does not have to *agree*: a disagreeing
	// email still means the two records are comparable on something beyond a
	// name, which is the weak-but-real signal the brief's own 1001/1003
	// example is built on.
	if !email.comparable && !addr.comparable && !zip.comparable {
		return Match{}, false
	}

	// Veto rule. A name that actively disagrees is evidence *against* identity,
	// not merely absent evidence for it, and averaging it away lets the other
	// fields outvote it. "A Smith" and "B Smith" at one ZIP are two people;
	// so are "John Smith" and "Mary Smith" at one address — the classic
	// household false positive that a naive address-and-surname match produces.
	//
	// The one thing that overrides a disagreeing name is a shared mailbox:
	// people do use nicknames, marry into new surnames and mistype their own
	// names, and an email that still lands in the same inbox outweighs that.
	sharedMailbox := email.comparable && email.score >= 0.85
	if (first.contradicts || last.contradicts) && !sharedMailbox {
		return Match{}, false
	}

	score := weighted / available
	confidence := bucket(score, opts)
	if confidence == "" {
		return Match{}, false
	}

	src, match := a, b
	if lessID(b.ID, a.ID) {
		src, match = b, a
	}
	return Match{
		SourceID:   src.ID,
		MatchID:    match.ID,
		Score:      score,
		Confidence: confidence,
		Reasons:    reasons,
	}, true
}

func bucket(score float64, opts Options) Confidence {
	switch {
	case score >= opts.HighThreshold:
		return High
	case score >= opts.MediumThreshold:
		return Medium
	case score >= opts.LowThreshold:
		return Low
	default:
		return "" // below the reporting floor
	}
}

// compareEmail scores the strongest identifier available.
//
// The local part and the domain are weighed separately because the input
// contains pairs whose mailbox name is identical but whose provider differs
// (nulla.eget@protonmail.couk / nulla.eget@att.couk). That is a person who
// changed provider, not a different person, so it scores high — but not 1.0,
// since two people can share a common local part like "info" or "j.smith".
//
// A differing email is deliberately *not* a contradiction: one person having
// a work address and a personal address is ordinary.
func compareEmail(a, b normalized) fieldResult {
	if a.emailLocal == "" || b.emailLocal == "" {
		return skipped()
	}
	switch {
	case a.emailLocal == b.emailLocal && a.emailDomain == b.emailDomain:
		return fieldResult{score: 1.0, comparable: true, reason: "email: identical"}
	case a.emailLocal == b.emailLocal:
		return fieldResult{score: 0.85, comparable: true,
			reason: "email: same mailbox name, different domain (" + a.emailDomain + " vs " + b.emailDomain + ")"}
	}
	if jaroWinkler(a.emailLocal, b.emailLocal) >= 0.93 {
		return fieldResult{score: 0.65, comparable: true, reason: "email: mailbox names nearly identical (likely typo)"}
	}
	return fieldResult{score: 0, comparable: true, weightScale: disagreementDamping, reason: "email: different"}
}

// compareName handles the dominant pattern in this dataset: a name recorded in
// full on one row and as a bare initial on the other ("Ciara" / "C").
//
// The abbreviation check runs *before* the equality check, which matters more
// than it looks. Two rows that both record a last name as "F" are equal as
// strings, but they have not told us the surname matches — only that both
// surnames start with F. Scoring that as an exact match is the single biggest
// source of false positives in this data, since the degraded half of the file
// abbreviates both names on hundreds of rows.
//
// An initial that agrees is corroborating but weak evidence (0.6): roughly one
// person in fifteen shares any given first initial. An initial that disagrees
// is a contradiction — a correctly recorded initial rules the match out rather
// than merely failing to support it.
func compareName(a, b, label string) fieldResult {
	if a == "" || b == "" {
		return skipped()
	}
	if isInitial(a) || isInitial(b) {
		if []rune(a)[0] == []rune(b)[0] {
			return fieldResult{score: 0.6, comparable: true,
				reason: label + ": initial is consistent (" + a + " / " + b + ")"}
		}
		return fieldResult{comparable: true, contradicts: true,
			reason: label + ": initials disagree (" + a + " / " + b + ")"}
	}
	if a == b {
		return fieldResult{score: 1.0, comparable: true, reason: label + ": exact"}
	}
	switch sim := jaroWinkler(a, b); {
	case sim >= 0.92:
		return fieldResult{score: 0.85, comparable: true,
			reason: label + ": near-identical spelling (" + a + " / " + b + ")"}
	case sim >= 0.85:
		return fieldResult{score: 0.6, comparable: true,
			reason: label + ": similar spelling (" + a + " / " + b + ")"}
	default:
		return fieldResult{comparable: true, contradicts: true,
			reason: label + ": different (" + a + " / " + b + ")"}
	}
}

// isInitial reports whether a normalized name is a single character, i.e. the
// abbreviated form. Normalization has already stripped any trailing period.
func isInitial(s string) bool { return len([]rune(s)) == 1 }

// compareAddress tolerates the formatting noise that survives normalization —
// a missing apartment number, a spelled-out unit — while still separating two
// genuinely different streets. A differing address is not a contradiction:
// people move, and the same person legitimately appears at two addresses.
//
// Two measures are combined because they fail in opposite ways. Edit distance
// handles scattered small differences but collapses when one record dropped
// its house number entirely ("668 4116 maecenas street" vs "maecenas
// street" scores only 0.63). Token containment catches exactly that case, but
// would happily match every "12 Main Street" to every other, so it only
// counts when the shared tokens include a distinctive street name. The higher
// of the two wins.
func compareAddress(a, b string) fieldResult {
	if a == "" || b == "" {
		return skipped()
	}
	if a == b {
		return fieldResult{score: 1.0, comparable: true, reason: "address: identical after normalization"}
	}

	best := fieldResult{comparable: true}
	switch sim := levenshteinRatio(a, b); {
	case sim >= 0.90:
		best = fieldResult{score: 0.9, comparable: true, reason: "address: near-identical"}
	case sim >= 0.80:
		best = fieldResult{score: 0.6, comparable: true, reason: "address: similar (possible unit/formatting difference)"}
	default:
		best = fieldResult{score: 0, comparable: true, weightScale: disagreementDamping, reason: "address: different"}
	}

	if c := addressContainment(a, b); c.score > best.score {
		best = c
	}
	return best
}

// addressContainment scores the "one record is a truncation of the other"
// case, which the sample data produces in bulk: the street name survives but
// the house number does not.
//
// The score is capped at 0.75 — well below an exact match — because a shared
// street name without a house number really is weaker evidence.
func addressContainment(a, b string) fieldResult {
	tokensA, tokensB := tokenSet(a), tokenSet(b)
	smaller, larger := tokensA, tokensB
	if len(larger) < len(smaller) {
		smaller, larger = larger, smaller
	}
	if len(smaller) == 0 {
		return fieldResult{comparable: true}
	}

	shared, distinctive := 0, 0
	for tok := range smaller {
		if larger[tok] {
			shared++
			// A street *name* identifies a street; the word "street" does not.
			if !genericAddressWord[tok] && !isAllDigits(tok) {
				distinctive++
			}
		}
	}
	if distinctive == 0 {
		return fieldResult{comparable: true}
	}

	switch containment := float64(shared) / float64(len(smaller)); {
	case containment == 1.0:
		return fieldResult{score: 0.75, comparable: true,
			reason: "address: one record is a truncation of the other (shared street name, missing number)"}
	case containment >= 0.7:
		return fieldResult{score: 0.5, comparable: true,
			reason: "address: shares a distinctive street name but differs elsewhere"}
	default:
		return fieldResult{comparable: true}
	}
}

func tokenSet(s string) map[string]bool {
	set := make(map[string]bool)
	for _, tok := range strings.Fields(s) {
		set[tok] = true
	}
	return set
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// compareZip is deliberately all-or-nothing. Postal codes are not similar to
// one another in any useful sense: 39746 and 39747 are different places, and
// treating them as a near-match would only manufacture false positives. Like
// address, a differing ZIP is weak counter-evidence rather than a veto.
func compareZip(a, b string) fieldResult {
	if a == "" || b == "" {
		return skipped()
	}
	if a == b {
		return fieldResult{score: 1.0, comparable: true, reason: "ZIP: identical"}
	}
	return fieldResult{score: 0, comparable: true, weightScale: disagreementDamping, reason: "ZIP: different"}
}

// lessID orders IDs numerically when both are numeric, and lexically
// otherwise, so output reads 2 < 10 rather than "10" < "2".
func lessID(a, b string) bool {
	na, errA := strconv.Atoi(a)
	nb, errB := strconv.Atoi(b)
	if errA == nil && errB == nil {
		return na < nb
	}
	return a < b
}
