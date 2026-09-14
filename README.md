# Contact deduplication

Identifies potentially duplicate contacts in a list and scores how confident
each match is. Written in Go, standard library only, all processing in memory.

## Running it

```
go test ./...                                  # the full test suite
go run ./cmd/dedupe -in input_sample.csv       # summary + all matches
go run ./cmd/dedupe -in input_sample.csv -min Medium -format csv -out matches.csv
go run ./cmd/dedupe -in input_sample.csv -explain 400   # why a contact matched
```

`matches.csv` in this directory is the output for the supplied input, in the
format the brief asks for: `ContactID Source, ContactID Match, Accuracy`, plus
the raw score.

## Results on the supplied input

1,000 contacts, 779 pairs reported:

| Accuracy | Reported | Correct | Precision |
|---------:|---------:|--------:|----------:|
| High     |      331 |     331 |    100.0% |
| Medium   |      165 |     165 |    100.0% |
| Low      |      283 |       4 |      1.4% |

**Recall 100%** — all 500 seeded duplicate pairs are found.
**Precision 100%** across High and Medium, the bands a caller would act on.

These are measured, not asserted: the supplied file turns out to be 500
contacts followed by a deliberately degraded copy of each, so contact *N* and
contact *N+500* are the same person. That gives a ground truth to score
against. No part of the matcher knows about this property — it is used only by
`TestOnSampleFile` to report precision and recall.

## How the scoring works

Each pair is compared field by field. Every field returns a score in `[0,1]`
plus a flag for whether it could be compared at all, and the results are
combined as a weighted average:

| Field   | Weight | Why |
|---------|-------:|-----|
| email   |   0.35 | Near-unique per person; the strongest identifier here |
| surname |   0.20 | Stable, and far more selective than a first name |
| address |   0.20 | Strong, but shared by everyone in a household |
| first name | 0.15 | Weak alone, and abbreviated on half the input rows |
| ZIP     |   0.10 | Weakest; thousands of unrelated people share one |

Scores of ≥0.82 read **High**, ≥0.60 **Medium**, ≥0.40 **Low**, and anything
below that is not reported.

Four decisions do most of the work.

**Missing fields are excluded, not penalised.** The average divides by the
weight of the fields actually present on *both* contacts, not by 1.0. About 5%
of rows are missing an email, a ZIP or an address, and without this they would
be systematically scored below complete rows for no reason. A blank is
"unknown", never "different" — two rows blank in the same place agree on
nothing. (`TestMissingFieldsDoNotPenalize`, `TestBlankFieldsAreNeverEvidence`)

**A name that disagrees vetoes the match.** Averaging lets four agreeing fields
outvote one disagreeing one, which is how a naive matcher decides that John
Smith and Mary Smith at one address are the same person. A contradicting name
rejects the pair outright instead. The exception is a shared mailbox — people
use nicknames and change surnames, and an email that still lands in the same
inbox outweighs a name that does not match.

**A detail that disagrees counts less than one that agrees.** Email, address
and ZIP are asymmetric evidence: a matching email is close to proof, while a
differing one proves little, since one person routinely has a work address and
a personal one, moves house, and leaves a stale ZIP on file. Disagreement in
these three counts at 40% weight. Without this, a single stale email hides real
duplicates.

**Names alone are never reported.** At least one contact detail must be present
on both sides. Two rows agreeing on "John Smith" and nothing else say that two
people have similar names. The sample input contains hundreds of such pairs.

### Two patterns the data is built around

*Abbreviated names.* Roughly 450 rows record a name as a bare initial. The
comparison checks for an initial **before** checking string equality, which
matters more than it looks: two rows that both record a surname as `F` are
equal as strings but have only told us both surnames start with F. Scoring that
as an exact match was the single largest source of false positives while
building this — fixing it moved precision from 80% to 91% in one change.

*Rewritten email domains.* `nulla.eget@protonmail.couk` and
`nulla.eget@att.couk` are one person who changed provider. The local part and
the domain are compared separately, so this scores 0.85 rather than 0 — high,
but not the 1.0 of an exact match, since two people can share a local part like
`info` or `j.smith`.

The hardest rows combine both, plus a truncated address and no email or ZIP at
all: contact 400 is `Baker Gallagher, 668-4116 Maecenas St.` and contact 900 is
`B G, Maecenas street.` These are caught by comparing address *tokens* for
containment as well as edit distance, and land at Medium (0.655) — correctly,
since two initials and a street name with no house number is a genuine lead but
not a certainty.

## What Low means, and why it is imprecise

Low is a review queue, not a claim. Its 1.4% precision is a deliberate
consequence of the brief.

The brief's worked example requires contacts 1001 and 1003 to be reported as
Low. That pair agrees on two initials, disagrees on email, and has nothing else
in common. It is the weakest evidence pattern the data contains — and 279
unrelated pairs in the input have *exactly the same shape*. No rule can surface
one and suppress the others; they are indistinguishable on the evidence.

So the reporting floor is set low enough to include it, and Low is labelled for
what it is. `-min Medium` filters it out, and `TestKnownWeakPair` pins the
trade-off down so it stays deliberate — including a check that an equivalent
unrelated pair is treated identically, so the model is not tuned to recognise
the example itself.

## Scaling

Comparing every contact against every other is O(n²) — 499,500 pairs for 1,000
contacts, 5 billion for 100,000. Instead each contact is filed in memory under
several cheap equality keys (mailbox name, full address, distinctive street
tokens, initial-plus-surname, ZIP-plus-initial, both initials), and only
contacts sharing a key are scored. On the sample this considers **5,141 pairs,
1.0% of the cross join**, with no loss of recall.

Several independent keys are used because each covers the others' blind spots:
a pair whose email was rewritten is still found by its address, and a pair that
moved house is still found by its email. A key that gathers more than
`MaxBlockSize` contacts is skipped — a bucket that large is not selective, and
expanding it costs quadratic time for almost no recall.

## Tests

`go test ./...` — 94.6% statement coverage on the matching package, 46% on
the CLI. Beyond the per-function tests:

- `TestAssessmentExample` reproduces the brief's worked example exactly.
- `TestOnSampleFile` measures precision and recall against the ground truth and
  enforces regression floors, including 99% precision on High — the band a
  caller would auto-merge on.
- `TestScoreIsSymmetric`, `TestDeterministic`, `TestNoSelfPairs`,
  `TestPairsReportedOnce` cover properties that are easy to break silently.
- `TestBlockingFindsPairsThroughAnyKey` checks each blocking key against a pair
  only it can find.

## Notes and limitations

- **Transitivity is not resolved.** If A matches B and B matches C, all three
  pairs are reported; they are not merged into a cluster. Clustering needs a
  merge policy (which record survives?) that belongs to the calling system.
- **Weights are hand-set**, chosen from how much each field narrows down a
  person. With labelled training data they could be fitted instead — logistic
  regression over the same field scores would be the natural next step, and the
  code is structured so only the combination step would change.
- **Names are compared as written.** A nickname table (Bob/Robert) and a
  phonetic key (Soundex, for Catherine/Katherine) would both help on real data;
  neither pattern appears in this input, so neither is implemented.
- **US ZIP codes only.** A postal code containing letters is treated as
  uncomparable rather than reduced to its digits — a Canadian "K1A 0B1" turned
  into "101" is not a parse failure but a wrong value that could collide with
  another mangled code. Real international support needs per-country rules.
- **`MaxBlockSize` can cost recall in principle.** A pair whose *only* shared
  blocking key sits in an oversized bucket is never scored. Several
  independent keys make this unlikely (and it costs nothing on this input),
  but it is a real trade-off, not a free optimization.
- The input contains its own data-quality artefacts — one address reads
  `Molestreetie road`, apparently from a find-and-replace of `st` → `street`
  applied to `Molestie`. Normalization expands abbreviations by whole token
  only, so it does not compound this kind of damage.
