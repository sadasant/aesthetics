// Package report defines the evaluation results, the CI policy that turns
// them into an exit code, and their text and JSON renderings.
package report

import (
	"fmt"
	"time"

	"github.com/sadasant/aesthetics/internal/decision"
	"github.com/sadasant/aesthetics/internal/guideline"
)

// Schema identifies the JSON results format.
const Schema = "aesthetics.report/v1"

// Exit codes.
const (
	ExitOK         = 0 // no failures at or above the threshold
	ExitFailed     = 1 // failures at or above the threshold
	ExitUsage      = 2 // invalid invocation or configuration
	ExitIncomplete = 3 // some evaluations could not be completed
)

// Outcome of one criterion.
type Outcome string

const (
	Pass          Outcome = "pass"
	Fail          Outcome = "fail"
	Indeterminate Outcome = "indeterminate"
)

// Artifact statuses.
const (
	StatusComplete = "complete"
	StatusPartial  = "partial" // some criteria or details could not be established
	StatusError    = "error"   // the artifact could not be evaluated
	StatusSkipped  = "skipped" // the artifact cannot be read as text
)

// Report is the result of one run.
type Report struct {
	Schema    string           `json:"schema"`
	Tool      Tool             `json:"tool"`
	Run       Run              `json:"run"`
	Artifacts []ArtifactResult `json:"artifacts"`
	Summary   Summary          `json:"summary"`
}

type Tool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Run records the inputs of the evaluation for reproducibility.
type Run struct {
	StartedAt  time.Time      `json:"started_at"`
	FinishedAt time.Time      `json:"finished_at"`
	Root       string         `json:"root"`
	Config     string         `json:"config,omitempty"`
	Presets    []string       `json:"presets"`
	Provider   *decision.Info `json:"provider,omitempty"`
	Diff       *DiffInfo      `json:"diff,omitempty"`
	Thresholds Thresholds     `json:"thresholds"`
	Policy     Policy         `json:"policy"`
	DryRun     bool           `json:"dry_run,omitempty"`
}

type DiffInfo struct {
	Label     string `json:"label"`
	Files     int    `json:"files"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

// Thresholds map a decision model's probability that a guideline is met to
// an outcome: pass at or above Pass, fail at or below Fail, indeterminate
// in between.
type Thresholds struct {
	Pass float64 `json:"pass"`
	Fail float64 `json:"fail"`
}

// Policy decides which results fail an automated run.
type Policy struct {
	// FailOn is the lowest severity whose failures fail the run; zero means never.
	FailOn              guideline.Severity `json:"-"`
	FailOnName          string             `json:"fail_on"`
	FailOnIndeterminate bool               `json:"fail_on_indeterminate"`
	FailOnIncomplete    bool               `json:"fail_on_incomplete"`
	FailOnPreexisting   bool               `json:"fail_on_preexisting"`
}

// ArtifactResult holds the results for one artifact.
type ArtifactResult struct {
	Kind   guideline.Kind `json:"kind"`
	Path   string         `json:"path"`
	Label  string         `json:"label"`
	Status string         `json:"status"`
	Error  string         `json:"error,omitempty"`
	Notes  []string       `json:"notes,omitempty"`
	// Facts are mechanically established; they involve no judgment.
	Facts   map[string]any `json:"facts"`
	Context []string       `json:"context,omitempty"`
	Results []Criterion    `json:"results"`
}

// Criterion is the evaluation of one guideline against one artifact.
//
// Probability and every field marked "model" are qualitative judgments made
// by the decision model. Explanation describes how Aesthetics derived the
// outcome from them. Evidence InChange flags are mechanical facts.
type Criterion struct {
	Guideline string             `json:"guideline"`
	Origin    string             `json:"origin"`
	Text      string             `json:"text"`
	Severity  guideline.Severity `json:"severity"`
	Priority  int                `json:"priority"`
	Rank      int                `json:"rank"`
	Outcome   Outcome            `json:"outcome"`
	// Probability is the model's estimate that the guideline is met.
	Probability    *float64 `json:"probability,omitempty"`
	Explanation    string   `json:"explanation"`
	Recommendation string   `json:"recommendation,omitempty"`
	// RecommendationBasis is "guideline" for the guideline's own
	// recommendation, or "model" when the model chose among its remedies.
	RecommendationBasis       string       `json:"recommendation_basis,omitempty"`
	RecommendationProbability *float64     `json:"recommendation_probability,omitempty"`
	Attribution               *Attribution `json:"attribution,omitempty"`
	Evidence                  []Evidence   `json:"evidence,omitempty"`
	YieldedTo                 string       `json:"yielded_to,omitempty"`
	YieldProbability          *float64     `json:"yield_probability,omitempty"`
	// Counted reports whether this result counts against the policy.
	Counted bool   `json:"counted"`
	Error   string `json:"error,omitempty"`
}

// Attribution says whether a failure was introduced by the evaluated change.
type Attribution struct {
	Value string `json:"value"` // introduced or preexisting
	// Basis is "model" (judged by the decision model), "scope" (diff
	// guidelines judge only the change), or "new file" (the file did not
	// exist before the change).
	Basis       string   `json:"basis"`
	Probability *float64 `json:"probability,omitempty"`
}

// Evidence points at the part of an artifact that best shows a failure.
type Evidence struct {
	Path        string  `json:"path"`
	StartLine   int     `json:"start_line,omitempty"`
	EndLine     int     `json:"end_line,omitempty"`
	Probability float64 `json:"probability"`
	// InChange mechanically reports whether the location overlaps lines
	// added or modified by the diff.
	InChange *bool `json:"in_change,omitempty"`
}

func (e Evidence) String() string {
	switch {
	case e.StartLine > 0 && e.EndLine > e.StartLine:
		return fmt.Sprintf("%s:%d-%d", e.Path, e.StartLine, e.EndLine)
	case e.StartLine > 0:
		return fmt.Sprintf("%s:%d", e.Path, e.StartLine)
	}
	return e.Path
}

// Summary aggregates the run.
type Summary struct {
	Artifacts     int    `json:"artifacts"`
	Complete      int    `json:"complete"`
	Partial       int    `json:"partial"`
	Errors        int    `json:"errors"`
	Skipped       int    `json:"skipped"`
	Criteria      int    `json:"criteria"`
	Pass          int    `json:"pass"`
	Fail          int    `json:"fail"`
	Indeterminate int    `json:"indeterminate"`
	Introduced    int    `json:"introduced"`
	Preexisting   int    `json:"preexisting"`
	Yielded       int    `json:"yielded"`
	Counted       int    `json:"counted"`
	Incomplete    bool   `json:"incomplete"`
	Status        string `json:"status"` // passed, failed, incomplete
	ExitCode      int    `json:"exit_code"`
}

// Finalize applies the policy: it marks counted results and computes the
// summary and exit code. Failures at or above the severity threshold count,
// except those that yielded to a dominant guideline and, unless configured
// otherwise, failures attributed to preexisting problems.
func (r *Report) Finalize() {
	p := r.Run.Policy
	s := Summary{Artifacts: len(r.Artifacts)}
	for i := range r.Artifacts {
		a := &r.Artifacts[i]
		switch a.Status {
		case StatusComplete:
			s.Complete++
		case StatusPartial:
			s.Partial++
		case StatusError:
			s.Errors++
		case StatusSkipped:
			s.Skipped++
		}
		for j := range a.Results {
			c := &a.Results[j]
			s.Criteria++
			atThreshold := p.FailOn != 0 && c.Severity >= p.FailOn
			c.Counted = false
			switch c.Outcome {
			case Pass:
				s.Pass++
			case Indeterminate:
				s.Indeterminate++
				c.Counted = atThreshold && p.FailOnIndeterminate
			case Fail:
				s.Fail++
				preexisting := c.Attribution != nil && c.Attribution.Value == "preexisting"
				switch {
				case preexisting:
					s.Preexisting++
				case c.Attribution != nil:
					s.Introduced++
				}
				if c.YieldedTo != "" {
					s.Yielded++
				}
				c.Counted = atThreshold && c.YieldedTo == "" && (!preexisting || p.FailOnPreexisting)
			}
			if c.Counted {
				s.Counted++
			}
		}
	}
	s.Incomplete = s.Partial+s.Errors > 0
	switch {
	case s.Counted > 0:
		s.Status, s.ExitCode = "failed", ExitFailed
	case s.Incomplete && p.FailOnIncomplete:
		s.Status, s.ExitCode = "incomplete", ExitIncomplete
	default:
		s.Status, s.ExitCode = "passed", ExitOK
	}
	r.Summary = s
}
