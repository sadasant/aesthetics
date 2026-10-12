package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sadasant/aesthetics/internal/decision"
)

type tally struct {
	clear, correct, falseFail, missedFail, indetClear int
	borderline, indetBorderline                       int
}

// score applies thresholds to the recorded probabilities.
func score(js []Judgment, passT, failT float64) tally {
	var t tally
	for _, j := range js {
		if j.Probability == nil {
			continue
		}
		p := *j.Probability
		outcome := "indeterminate"
		switch {
		case p >= passT:
			outcome = "pass"
		case p <= failT:
			outcome = "fail"
		}
		switch j.Expect {
		case "borderline":
			t.borderline++
			if outcome == "indeterminate" {
				t.indetBorderline++
			}
		default:
			t.clear++
			switch {
			case outcome == j.Expect:
				t.correct++
			case outcome == "indeterminate":
				t.indetClear++
			case j.Expect == "pass":
				t.falseFail++
			default:
				t.missedFail++
			}
		}
	}
	return t
}

// objective rewards correct decisive outcomes and penalizes false failures
// most, since they block work and erode trust in CI; missed failures cost
// less, and indeterminate results on clear cases cost a little.
func (t tally) objective() float64 {
	return float64(t.correct) - 3*float64(t.falseFail) - 1.5*float64(t.missedFail) - 0.5*float64(t.indetClear)
}

func pct(n, d int) string {
	if d == 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", 100*float64(n)/float64(d))
}

// auc is the probability that a random case labeled pass receives a higher
// probability than a random case labeled fail: threshold-free separation.
func auc(js []Judgment) float64 {
	var pos, neg []float64
	for _, j := range js {
		if j.Probability == nil {
			continue
		}
		switch j.Expect {
		case "pass":
			pos = append(pos, *j.Probability)
		case "fail":
			neg = append(neg, *j.Probability)
		}
	}
	if len(pos) == 0 || len(neg) == 0 {
		return 0
	}
	var s float64
	for _, p := range pos {
		for _, n := range neg {
			switch {
			case p > n:
				s++
			case p == n:
				s += 0.5
			}
		}
	}
	return s / float64(len(pos)*len(neg))
}

func mean(js []Judgment, expect string) string {
	var s float64
	n := 0
	for _, j := range js {
		if j.Probability != nil && j.Expect == expect {
			s += *j.Probability
			n++
		}
	}
	if n == 0 {
		return "-"
	}
	return fmt.Sprintf("%.2f", s/float64(n))
}

func printTally(label string, t tally) {
	fmt.Printf("%-12s clear %3d · correct %4s · false fail %4s · missed fail %4s · indeterminate %4s · borderline indeterminate %s\n",
		label, t.clear, pct(t.correct, t.clear), pct(t.falseFail, t.clear), pct(t.missedFail, t.clear), pct(t.indetClear, t.clear),
		pct(t.indetBorderline, t.borderline))
}

func printReport(js []Judgment, passT, failT float64, info decision.Info, c *cache) {
	fmt.Printf("Aesthetics evals · %s · %d judgments · thresholds pass ≥ %.2f, fail ≤ %.2f\n", info.Label(), len(js), passT, failT)
	fmt.Printf("provider calls %d (cache hits %d) · input tokens %d\n\n", c.calls, c.hits, c.tokens)

	var errs []Judgment
	for _, j := range js {
		if j.Error != "" {
			errs = append(errs, j)
		}
	}
	printTally("overall", score(js, passT, failT))
	bySet := map[string][]Judgment{}
	for _, j := range js {
		set := strings.SplitN(j.Guideline, "/", 2)[0]
		bySet[set] = append(bySet[set], j)
	}
	sets := make([]string, 0, len(bySet))
	for s := range bySet {
		sets = append(sets, s)
	}
	sort.Strings(sets)
	for _, s := range sets {
		printTally(s, score(bySet[s], passT, failT))
	}
	fmt.Printf("\nseparation: AUC %.2f · mean probability for pass-labeled %s, fail-labeled %s, borderline %s\n",
		auc(js), mean(js, "pass"), mean(js, "fail"), mean(js, "borderline"))

	var attr, attrOK, ev, evOK int
	for _, j := range js {
		if j.WantAttrib != "" && j.Outcome == "fail" {
			attr++
			if j.Attribution == j.WantAttrib {
				attrOK++
			}
		}
		if j.EvidenceHit != nil {
			ev++
			if *j.EvidenceHit {
				evOK++
			}
		}
	}
	fmt.Printf("attribution correct %s of %d failing diff judgments · evidence on the flaw %s of %d\n", pct(attrOK, attr), attr, pct(evOK, ev), ev)

	fmt.Println("\nmisjudgments at these thresholds:")
	for _, j := range js {
		if j.Probability == nil || j.Expect == "borderline" {
			continue
		}
		if j.Outcome != j.Expect || (j.WantAttrib != "" && j.Outcome == "fail" && j.Attribution != j.WantAttrib) {
			note := ""
			if j.WantAttrib != "" && j.Outcome == "fail" && j.Attribution != j.WantAttrib {
				note = fmt.Sprintf(" (attribution %s, want %s)", j.Attribution, j.WantAttrib)
			}
			fmt.Printf("  %-44s %-36s want %-4s got %-13s p=%.2f%s\n", j.Case, j.Guideline, j.Expect, j.Outcome, *j.Probability, note)
		}
	}
	fmt.Println("\nborderline cases:")
	for _, j := range js {
		if j.Expect == "borderline" && j.Probability != nil {
			fmt.Printf("  %-44s %-36s p=%.2f\n", j.Case, j.Guideline, *j.Probability)
		}
	}
	if len(errs) > 0 {
		fmt.Println("\nerrors:")
		for _, j := range errs {
			fmt.Printf("  %s %s: %s\n", j.Case, j.Guideline, j.Error)
		}
	}

	type cand struct {
		pass, fail float64
		t          tally
	}
	var cands []cand
	for p := 50; p <= 95; p += 5 {
		for f := 5; f < p && f <= 50; f += 5 {
			cands = append(cands, cand{float64(p) / 100, float64(f) / 100, score(js, float64(p)/100, float64(f)/100)})
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].t.objective() > cands[j].t.objective() })
	fmt.Println("\nthreshold sweep (best first; objective = correct − 3·false fail − 1.5·missed fail − 0.5·indeterminate):")
	for i, cd := range cands {
		if i == 8 {
			break
		}
		t := cd.t
		fmt.Printf("  pass ≥ %.2f fail ≤ %.2f · objective %5.1f · correct %s · false fail %d · missed fail %d · indeterminate %d · borderline indeterminate %s\n",
			cd.pass, cd.fail, t.objective(), pct(t.correct, t.clear), t.falseFail, t.missedFail, t.indetClear, pct(t.indetBorderline, t.borderline))
	}
}
