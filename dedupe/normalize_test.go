package dedupe

import "testing"

func TestNormName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Ciara", "ciara"},
		{"  Ciara  ", "ciara"},
		{"CIARA", "ciara"},
		{"C.", "c"}, // abbreviated form loses its period
		{"C", "c"},  // ...and matches the bare initial
		{"Mary-Jane", "mary jane"},
		{"O'Brien", "o brien"},
		{"José", "jose"}, // accents fold to the base letter
		{"", ""},
		{"   ", ""},
	}
	for _, tc := range tests {
		if got := normName(tc.in); got != tc.want {
			t.Errorf("normName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSplitEmail(t *testing.T) {
	tests := []struct{ in, local, domain string }{
		{"mollis.lectus.pede@outlook.net", "mollis.lectus.pede", "outlook.net"},
		{"  Mollis.Lectus.Pede@Outlook.NET ", "mollis.lectus.pede", "outlook.net"},
		{"nulla.eget@protonmail.couk", "nulla.eget", "protonmail.couk"},
		{"nulla.eget@att.couk", "nulla.eget", "att.couk"},  // same local, other domain
		{"user+newsletter@gmail.com", "user", "gmail.com"}, // +tag addresses one mailbox
		{"", "", ""},
		{"not-an-email", "not-an-email", ""}, // kept rather than discarded
	}
	for _, tc := range tests {
		local, domain := splitEmail(tc.in)
		if local != tc.local || domain != tc.domain {
			t.Errorf("splitEmail(%q) = (%q, %q), want (%q, %q)", tc.in, local, domain, tc.local, tc.domain)
		}
	}
}

func TestNormZip(t *testing.T) {
	tests := []struct{ in, want string }{
		{"39746", "39746"},
		{" 39746 ", "39746"},
		{"39746-1234", "39746"}, // ZIP+4 truncated to the base code
		{"01234", "01234"},      // leading zero preserved: ZIPs are text
		{"", ""},
		{"N/A", ""},
	}
	for _, tc := range tests {
		if got := normZip(tc.in); got != tc.want {
			t.Errorf("normZip(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormAddress(t *testing.T) {
	tests := []struct{ in, want string }{
		{"449-6990 Tellus. Rd.", "449 6990 tellus road"},
		{"449 6990 Tellus Road", "449 6990 tellus road"}, // both spellings converge
		{"Ap #312-8611 Lacus. Ave", "apartment 312 8611 lacus avenue"},
		{"P.O. Box 775, 8910 Arcu. Road", "po box 775 8910 arcu road"},
		{"4811 Aliquam St.", "4811 aliquam street"},
		{"123 N Main St", "123 north main street"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := normAddress(tc.in); got != tc.want {
			t.Errorf("normAddress(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestNormalizationConverges is the point of the whole file: differently
// formatted records of one contact must produce identical normalized forms.
func TestNormalizationConverges(t *testing.T) {
	a := normalize(Contact{FirstName: "Ciara", LastName: "French",
		Email: "Mollis.Lectus.Pede@Outlook.net", Zip: "39746-0001", Address: "449-6990 Tellus. Rd."})
	b := normalize(Contact{FirstName: " ciara ", LastName: "FRENCH",
		Email: "mollis.lectus.pede@outlook.NET", Zip: " 39746 ", Address: "449 6990 Tellus Road"})

	if a != b {
		t.Errorf("equivalent records normalized differently:\n  %+v\n  %+v", a, b)
	}
}
