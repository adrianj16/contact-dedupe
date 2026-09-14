// Package dedupe identifies potentially duplicate contacts in a list.
//
// The whole pipeline runs in working memory: contacts are loaded into a slice,
// normalized once, indexed with in-memory blocking keys and scored pairwise.
// No database or external service is involved.
package dedupe

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
)

// Contact is one row of the input list, exactly as supplied.
//
// Every field except ID is optional: the sample data is missing an email, a
// postal code or a street address on roughly 5% of rows, so the matcher must
// treat "absent" as "unknown", never as "different".
type Contact struct {
	ID        string
	FirstName string
	LastName  string
	Email     string
	Zip       string
	Address   string

	// norm holds the pre-computed normalized form. Normalizing once at load
	// time keeps the O(candidate pairs) scoring loop free of string churn.
	norm normalized
}

// expected CSV header, in the order the assessment packet supplies it.
var wantHeader = []string{"contactID", "name", "name1", "email", "postalZip", "address"}

// LoadCSV reads contacts from r. The header row is validated so that a
// re-ordered or unexpected file fails loudly instead of silently matching on
// the wrong columns.
func LoadCSV(r io.Reader) ([]Contact, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = len(wantHeader)
	cr.TrimLeadingSpace = true

	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}
	if err := checkHeader(header); err != nil {
		return nil, err
	}

	var contacts []Contact
	for line := 2; ; line++ {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading line %d: %w", line, err)
		}
		c := Contact{
			ID:        strings.TrimSpace(rec[0]),
			FirstName: rec[1],
			LastName:  rec[2],
			Email:     rec[3],
			Zip:       rec[4],
			Address:   rec[5],
		}
		if c.ID == "" {
			return nil, fmt.Errorf("line %d: empty contactID", line)
		}
		c.norm = normalize(c)
		contacts = append(contacts, c)
	}
	return contacts, nil
}

func checkHeader(got []string) error {
	if len(got) != len(wantHeader) {
		return fmt.Errorf("header: got %d columns, want %d %v", len(got), len(wantHeader), wantHeader)
	}
	for i, want := range wantHeader {
		// The exported file carries a UTF-8 BOM on the first cell in some
		// spreadsheet exports, so compare loosely.
		if !strings.EqualFold(strings.Trim(strings.TrimSpace(got[i]), "\ufeff"), want) {
			return fmt.Errorf("header column %d: got %q, want %q", i+1, got[i], want)
		}
	}
	return nil
}
