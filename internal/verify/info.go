package verify

import (
	"encoding/json"
	"strings"
)

// Info is the verify object of a diagnostic in JSON (design section
// 5.5). Fields are added, never renamed: agents read them.
type Info struct {
	Obligation   string            `json:"obligation"`
	Fn           string            `json:"fn"`
	Panic        string            `json:"panic"`
	Class        string            `json:"class"`
	Rule         string            `json:"rule,omitempty"`
	Status       string            `json:"status"`
	Reason       string            `json:"reason,omitempty"`
	Confirmed    string            `json:"confirmed,omitempty"`
	Engine       string            `json:"engine"`
	Condition    string            `json:"condition,omitempty"`
	Input        map[string]string `json:"input,omitempty"`
	PanicMessage string            `json:"panicMessage,omitempty"`
	ReproducedBy []string          `json:"reproducedBy,omitempty"`
	Assumes      []string          `json:"assumes"`
	Test         *TestRef          `json:"test,omitempty"`
	// Label is the text under the primary span: what fails and when.
	Label string `json:"label,omitempty"`
}

// TestRef names a generated test.
type TestRef struct {
	File string `json:"file"`
	Name string `json:"name"`
}

// Summary is the verify object of the result line: counts over the run.
type Summary struct {
	Functions      int `json:"functions"`
	Obligations    int `json:"obligations"`
	Proved         int `json:"proved"`
	ProvedAssuming int `json:"provedAssuming"`
	Recovered      int `json:"recovered"`
	Manifest       int `json:"manifest"`
	Latent         int `json:"latent"`
	Unknown        int `json:"unknown"`
	// Counterexamples are panics the search ran into, confirmed by
	// running the input again.
	Counterexamples int `json:"counterexamples,omitempty"`
	// Notes say what limited the run: no sandbox, a search that could
	// not build. Zero counterexamples mean nothing without them.
	Notes []string `json:"notes,omitempty"`
}

// Add sums s2 into s.
func (s *Summary) Add(s2 Summary) {
	s.Functions += s2.Functions
	s.Obligations += s2.Obligations
	s.Proved += s2.Proved
	s.ProvedAssuming += s2.ProvedAssuming
	s.Recovered += s2.Recovered
	s.Manifest += s2.Manifest
	s.Latent += s2.Latent
	s.Unknown += s2.Unknown
	s.Counterexamples += s2.Counterexamples
}

// Summary counts the statuses of r.
func (r *Result) Summary() Summary {
	var s Summary
	for _, f := range r.Funcs {
		s.Functions++
		for _, o := range f.Obligations {
			if o.Class == "manifest" {
				s.Manifest++
			}
			if o.Via != nil {
				continue
			}
			s.Obligations++
			switch o.Status {
			case Proved:
				s.Proved++
			case ProvedAssuming:
				s.ProvedAssuming++
			case Recovered:
				s.Recovered++
			case Latent:
				s.Latent++
			case Unknown:
				s.Unknown++
			}
		}
	}
	return s
}

// The categories of the analyzer's diagnostics: a finding carries its
// Info as JSON after the first line, so it survives the check engine's
// cache; the statistics diagnostic carries a Summary.
const (
	CategoryFinding = "verify"
	CategoryStats   = "verify-stats"
)

func encodeCategory(kind string, v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return kind
	}
	return kind + "\n" + string(b)
}

// DecodeCategory splits a diagnostic category into its kind and JSON
// payload.
func DecodeCategory(c string) (kind string, payload json.RawMessage) {
	kind, rest, ok := strings.Cut(c, "\n")
	if !ok {
		return c, nil
	}
	return kind, json.RawMessage(rest)
}

// info builds the JSON object of one open obligation.
func info(f *FuncResult, o *ObResult) Info {
	ob := o.Ob
	in := Info{
		Obligation: ob.Key,
		Fn:         f.Name,
		Panic:      string(ob.Kind),
		Class:      o.Class,
		Rule:       o.Rule,
		Status:     string(o.Status),
		Reason:     o.Reason,
		Engine:     "pentagon",
		Condition:  o.Condition,
		Assumes:    append([]string{"no-races"}, o.Assumes...),
	}
	if in.Class == "" {
		in.Class = "latent"
	}
	if o.Status == Counterexample {
		in.Confirmed = "none"
	}
	return in
}
