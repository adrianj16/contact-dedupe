// Command dedupe reads a contact CSV and writes the candidate duplicate pairs.
//
//	go run ./cmd/dedupe -in input_sample.csv
//	go run ./cmd/dedupe -in input_sample.csv -format csv -out matches.csv
//	go run ./cmd/dedupe -in input_sample.csv -explain -min Medium
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/adrianugas/contact-dedupe/dedupe"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "dedupe:", err)
		os.Exit(1)
	}
}

func run() error {
	in := flag.String("in", "input_sample.csv", "input contact CSV")
	out := flag.String("out", "", "output file (default: stdout)")
	format := flag.String("format", "table", "output format: table or csv")
	explain := flag.String("explain", "", "print the per-field reasoning for matches involving this contact ID")
	minConf := flag.String("min", "Low", "minimum accuracy to report: High, Medium or Low")
	flag.Parse()

	f, err := os.Open(*in)
	if err != nil {
		return err
	}
	defer f.Close()

	contacts, err := dedupe.LoadCSV(f)
	if err != nil {
		return err
	}

	matches := dedupe.FindDuplicates(contacts, dedupe.DefaultOptions)
	matches = filterByConfidence(matches, *minConf)

	var w io.Writer = os.Stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}

	if *explain != "" {
		return writeExplanation(w, matches, *explain)
	}
	switch *format {
	case "csv":
		return writeCSV(w, matches)
	case "table":
		return writeTable(w, matches, len(contacts))
	default:
		return fmt.Errorf("unknown -format %q (want table or csv)", *format)
	}
}

// rank orders the accuracy labels so -min can filter on them.
var rank = map[dedupe.Confidence]int{dedupe.Low: 1, dedupe.Medium: 2, dedupe.High: 3}

func filterByConfidence(matches []dedupe.Match, min string) []dedupe.Match {
	floor := rank[dedupe.Confidence(titleCase(min))]
	if floor == 0 {
		floor = rank[dedupe.Low]
	}
	kept := matches[:0]
	for _, m := range matches {
		if rank[m.Confidence] >= floor {
			kept = append(kept, m)
		}
	}
	return kept
}

// titleCase upper-cases the first letter so -min accepts "high" or "High".
// (strings.Title is deprecated and Unicode-aware casing is overkill here.)
func titleCase(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// writeCSV emits exactly the columns the assessment asks for, plus the raw
// score so a reviewer can see where each label came from.
func writeCSV(f io.Writer, matches []dedupe.Match) error {
	cw := csv.NewWriter(f)
	defer cw.Flush()
	if err := cw.Write([]string{"ContactID Source", "ContactID Match", "Accuracy", "Score"}); err != nil {
		return err
	}
	for _, m := range matches {
		rec := []string{m.SourceID, m.MatchID, string(m.Confidence), strconv.FormatFloat(m.Score, 'f', 3, 64)}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	return cw.Error()
}

func writeTable(f io.Writer, matches []dedupe.Match, total int) error {
	counts := map[dedupe.Confidence]int{}
	for _, m := range matches {
		counts[m.Confidence]++
	}
	fmt.Fprintf(f, "%d contacts scanned, %d candidate duplicate pairs found\n", total, len(matches))
	fmt.Fprintf(f, "  High: %d   Medium: %d   Low: %d\n\n", counts[dedupe.High], counts[dedupe.Medium], counts[dedupe.Low])
	fmt.Fprintf(f, "%-18s %-18s %-10s %s\n", "ContactID Source", "ContactID Match", "Accuracy", "Score")
	fmt.Fprintf(f, "%s\n", strings.Repeat("-", 58))
	for _, m := range matches {
		fmt.Fprintf(f, "%-18s %-18s %-10s %.3f\n", m.SourceID, m.MatchID, m.Confidence, m.Score)
	}
	return nil
}

// writeExplanation dumps the field-by-field reasoning behind every match that
// involves one contact. This is the view used to audit a disputed decision.
func writeExplanation(f io.Writer, matches []dedupe.Match, id string) error {
	found := false
	for _, m := range matches {
		if m.SourceID != id && m.MatchID != id {
			continue
		}
		found = true
		fmt.Fprintf(f, "%s <-> %s   %s (%.3f)\n", m.SourceID, m.MatchID, m.Confidence, m.Score)
		for _, r := range m.Reasons {
			fmt.Fprintf(f, "    - %s\n", r)
		}
		fmt.Fprintln(f)
	}
	if !found {
		fmt.Fprintf(f, "no matches reported for contact %s\n", id)
	}
	return nil
}
