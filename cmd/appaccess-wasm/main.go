// Command appaccess-wasm exposes the app-access rule evaluator to JavaScript
// when built for the js/wasm target. It registers a global
// evaluateAppAccessRule function that the accompanying web page calls.
//
// Build with:
//
//	GOOS=js GOARCH=wasm go build -o web/app-access/eval.wasm ./cmd/appaccess-wasm

//go:build js && wasm

package main

import (
	"syscall/js"

	"gopkg.in/yaml.v3"

	"github.com/juliaogris/teleport-expressions/appaccess"
)

func main() {
	js.Global().Set("evaluateAppAccessRule", js.FuncOf(evaluate))
	js.Global().Set("desugarAppResources", js.FuncOf(desugar))
	// Block forever so the registered functions stay callable.
	select {}
}

// desugar is called from JavaScript as desugarAppResources(resourcesYAML). It
// returns an object with a yaml field holding the desugared app_resources, or
// a string error field on failure.
func desugar(this js.Value, args []js.Value) any {
	if len(args) != 1 {
		return map[string]any{"error": "expected one argument: app_resources YAML"}
	}
	out, err := appaccess.Desugar(args[0].String())
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"yaml": out}
}

// evaluate is called from JavaScript as evaluateAppAccessRule(ruleYAML,
// inputYAML). It returns an object with an allowed boolean and a vars object on
// a match, or a string error field on failure.
func evaluate(this js.Value, args []js.Value) any {
	if len(args) != 2 {
		return errResult("expected two arguments: rule YAML and input YAML")
	}
	ruleYAML := args[0].String()
	inputYAML := args[1].String()

	var in appaccess.Input
	if err := yaml.Unmarshal([]byte(inputYAML), &in); err != nil {
		return errResult("parsing input YAML: " + err.Error())
	}

	res, err := appaccess.Evaluate(ruleYAML, in)
	if err != nil {
		return errResult(err.Error())
	}

	vars := map[string]any{}
	for k, v := range res.Vars {
		vars[k] = v
	}
	denyHints := make([]any, 0, len(res.DenyHints))
	for _, h := range res.DenyHints {
		denyHints = append(denyHints, map[string]any{
			"denyCode":   h.DenyCode,
			"denyReason": h.DenyReason,
		})
	}
	return map[string]any{
		"allowed":     res.Allowed,
		"vars":        vars,
		"allowCode":   res.AllowCode,
		"allowReason": res.AllowReason,
		"denyKind":    res.DenyKind,
		"denyHints":   denyHints,
	}
}

func errResult(msg string) map[string]any {
	return map[string]any{"allowed": false, "error": msg}
}
