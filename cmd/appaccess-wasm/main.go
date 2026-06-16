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
	// Block forever so the registered function stays callable.
	select {}
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
	return map[string]any{"allowed": res.Allowed, "vars": vars}
}

func errResult(msg string) map[string]any {
	return map[string]any{"allowed": false, "error": msg}
}
