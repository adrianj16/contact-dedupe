package dedupe

import (
	"regexp"
	"strings"
	"unicode"
)

// normalized is the comparison-ready form of a Contact. Every field is lower
// case, punctuation-free and whitespace-collapsed, so that "P.O. Box 775" and
// "PO BOX 775" compare equal without any work at scoring time.
//
// An empty string always means "the source value was absent or blank" — it is
// never a legitimate comparable value.
type normalized struct {
	first       string // "ciara"
	last        string // "french"
	emailLocal  string // "mollis.lectus.pede"
	emailDomain string // "outlook.net"
	zip         string // "39746"
	address     string // "449 6990 tellus road"
}

func normalize(c Contact) normalized {
	local, domain := splitEmail(c.Email)
	return normalized{
		first:       normName(c.FirstName),
		last:        normName(c.LastName),
		emailLocal:  local,
		emailDomain: domain,
		zip:         normZip(c.Zip),
		address:     normAddress(c.Address),
	}
}

// normName lower-cases a personal name and strips punctuation, so that the
// abbreviated forms in the data ("C.", "C") collapse to the same token ("c").
// Accents are folded to their base letter so "Jose" and "José" agree.
func normName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(foldAccent(r))
		case r == ' ' || r == '-' || r == '\'':
			b.WriteRune(' ')
		}
		// everything else (periods in "C.", commas) is dropped
	}
	return collapseSpaces(b.String())
}

// splitEmail lower-cases an address and returns its local part and domain.
//
// Splitting matters because the data contains contacts whose local part is
// stable but whose domain was rewritten (nulla.eget@protonmail.couk vs
// nulla.eget@att.couk). An exact-string comparison would miss those entirely;
// comparing the halves separately lets the scorer treat it as strong-but-not-
// certain evidence.
//
// A "+tag" suffix is stripped from the local part, since it addresses the same
// mailbox. Dots are deliberately left alone: Gmail ignores them, but most mail
// systems treat them as significant, so collapsing them would merge genuinely
// distinct addresses.
func splitEmail(s string) (local, domain string) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "", ""
	}
	at := strings.LastIndex(s, "@")
	if at < 0 {
		// Malformed address: keep it as a local part rather than discarding
		// the only identifying value on the row.
		return canonLocal(s), ""
	}
	return canonLocal(s[:at]), strings.Trim(s[at+1:], ". ")
}

func canonLocal(s string) string {
	if plus := strings.Index(s, "+"); plus > 0 {
		s = s[:plus]
	}
	return strings.TrimSpace(s)
}

// normZip canonicalizes a US ZIP code. Postal codes are compared as text, not
// numbers, so a leading zero ("01234") is preserved. A ZIP+4 is truncated to
// its first five digits so that "39746" and "39746-1234" agree.
//
// A value containing letters is rejected outright rather than stripped down to
// its digits. This matters: a Canadian "K1A 0B1" reduced to "101" is not a
// failure to parse, it is a wrong value that can collide with another mangled
// code and manufacture a false ZIP match. Refusing to guess makes the field
// simply uncomparable, which the scorer already handles correctly.
//
// The supplied input is entirely US ZIPs. Supporting international formats
// properly means per-country rules, which is out of scope here.
func normZip(s string) string {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return ""
		}
	}
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	z := b.String()
	if len(z) > 5 {
		z = z[:5]
	}
	return z
}

// streetAbbrev maps the abbreviations present in the sample data to a single
// canonical spelling, so "449-6990 Tellus. Rd." and "449 6990 Tellus Road"
// normalize identically.
var streetAbbrev = map[string]string{
	"st": "street", "str": "street",
	"rd":  "road",
	"ave": "avenue", "av": "avenue",
	"dr":   "drive",
	"ln":   "lane",
	"blvd": "boulevard",
	"ct":   "court",
	"pl":   "place",
	"hwy":  "highway",
	"pkwy": "parkway",
	"cir":  "circle",
	"ap":   "apartment", "apt": "apartment",
	"ste": "suite",
	"n":   "north", "s": "south", "e": "east", "w": "west",
	"ne": "northeast", "nw": "northwest", "se": "southeast", "sw": "southwest",
}

// poBox matches the punctuated forms of "P.O. Box". It is applied before
// punctuation is stripped, because stripping first would split "P.O." into two
// stray single letters ("p o box") that no abbreviation rule can put back
// together.
var poBox = regexp.MustCompile(`\bp\.?\s*o\.?\s*box\b`)

// normAddress lower-cases a street address, replaces punctuation with spaces
// and expands known abbreviations. Digits are kept as-is because house and
// box numbers carry most of an address's identifying power.
func normAddress(s string) string {
	lower := poBox.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "pobox")

	var b strings.Builder
	for _, r := range lower {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(foldAccent(r))
		} else {
			b.WriteRune(' ')
		}
	}
	fields := strings.Fields(b.String())
	for i, f := range fields {
		if f == "pobox" {
			fields[i] = "po box"
			continue
		}
		if full, ok := streetAbbrev[f]; ok {
			fields[i] = full
		}
	}
	return strings.Join(fields, " ")
}

func collapseSpaces(s string) string { return strings.Join(strings.Fields(s), " ") }

// foldAccent maps the Latin-1 accented letters to their unaccented base. The
// sample data is ASCII, but real contact lists rarely are.
func foldAccent(r rune) rune {
	if base, ok := accentFolds[r]; ok {
		return base
	}
	return r
}

var accentFolds = map[rune]rune{
	'\u00e1': 'a', '\u00e0': 'a', '\u00e2': 'a', '\u00e4': 'a', '\u00e3': 'a', '\u00e5': 'a',
	'\u00e9': 'e', '\u00e8': 'e', '\u00ea': 'e', '\u00eb': 'e',
	'\u00ed': 'i', '\u00ec': 'i', '\u00ee': 'i', '\u00ef': 'i',
	'\u00f3': 'o', '\u00f2': 'o', '\u00f4': 'o', '\u00f6': 'o', '\u00f5': 'o',
	'\u00fa': 'u', '\u00f9': 'u', '\u00fb': 'u', '\u00fc': 'u',
	'\u00f1': 'n', '\u00e7': 'c', '\u00fd': 'y', '\u00ff': 'y',
}
