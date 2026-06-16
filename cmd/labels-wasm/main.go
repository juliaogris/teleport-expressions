// Command labels-wasm exposes the label expression evaluator to JavaScript when
// built for the js/wasm target. It registers a global evaluateLabelExpression
// function that the accompanying web page calls.
//
// Build with:
//
//	GOOS=js GOARCH=wasm go build -o web/labels/eval.wasm ./cmd/labels-wasm

//go:build js && wasm

package main

import (
	"syscall/js"

	"gopkg.in/yaml.v3"

	"github.com/juliaogris/teleport-expressions/labelexpr"
)

func main() {
	js.Global().Set("evaluateLabelExpression", js.FuncOf(evaluate))
	// Block forever so the registered function stays callable.
	select {}
}

// evaluate is called from JavaScript as
// evaluateLabelExpression(expr, inputYAML). It returns an object with either a
// boolean match field or a string error field.
func evaluate(this js.Value, args []js.Value) any {
	if len(args) != 2 {
		return result(false, "expected two arguments: expression and YAML input")
	}
	expr := args[0].String()
	rawInput := args[1].String()

	var in labelexpr.Input
	if err := yaml.Unmarshal([]byte(rawInput), &in); err != nil {
		return result(false, "parsing YAML input: "+err.Error())
	}

	match, err := labelexpr.Evaluate(expr, in)
	if err != nil {
		return result(false, err.Error())
	}
	return result(match, "")
}

func result(match bool, errMsg string) map[string]any {
	out := map[string]any{"match": match}
	if errMsg != "" {
		out["error"] = errMsg
	}
	return out
}
