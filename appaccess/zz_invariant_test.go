package appaccess

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

type sample struct {
	Topic    string `json:"topic"`
	Examples []struct {
		Name  string `json:"name"`
		Rule  string `json:"rule"`
		Input string `json:"input"`
	} `json:"examples"`
}

// outcome is the user-visible result of a mode: either an error, or an
// allow/deny with its codes and captures.
func outcome(rule string, in Input) string {
	res, err := Evaluate(rule, in)
	if err != nil {
		return "ERROR"
	}
	return fmt.Sprintf("allowed=%v allow=%q deny=%q vars=%v", res.Allowed, res.AllowCode, res.DenyKind, res.Vars)
}

func TestSugaredEqualsDesugared(t *testing.T) {
	data, err := os.ReadFile("../web/app-access/samples.json")
	if err != nil {
		t.Fatal(err)
	}
	var topics []sample
	if err := json.Unmarshal(data, &topics); err != nil {
		t.Fatal(err)
	}
	total, bad := 0, 0
	for ti, tp := range topics {
		for ei, ex := range tp.Examples {
			total++
			var in Input
			if err := yaml.Unmarshal([]byte(ex.Input), &in); err != nil {
				t.Errorf("t%d/e%d %q: bad input yaml: %v", ti, ei, ex.Name, err)
				bad++
				continue
			}
			sug := outcome(ex.Rule, in)
			// Desugared mode: lower first, then evaluate. A failure at either
			// step is the error the user sees.
			var des string
			if d, derr := Desugar(ex.Rule); derr != nil {
				des = "ERROR"
			} else {
				des = outcome(d, in)
			}
			if sug != des {
				fmt.Printf("MISMATCH t%d/e%d %-44s\n  sugared:   %s\n  desugared: %s\n", ti, ei, ex.Name, sug, des)
				bad++
			}
		}
	}
	fmt.Printf("\nchecked %d examples, %d mismatches\n", total, bad)
	if bad > 0 {
		t.Fatalf("%d examples differ", bad)
	}
}
