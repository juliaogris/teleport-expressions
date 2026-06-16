# teleport-expressions

A small standalone evaluator for [Teleport](https://goteleport.com) label
expressions. It reuses Teleport's `typical` predicate library and reproduces the
label expression parser specification, then exposes it three ways: a
command-line tool, a Go package, and a WebAssembly module driven by a minimal
web page.

Label expressions are the predicate language used in Teleport roles to match
resources by their labels, for example:

```
labels["env"] == "prod" && contains(user.spec.traits["groups"], labels["owner"])
```

## Layout

- `labelexpr/` - the evaluator package and its public `Evaluate` function.
- `internal/typical/` - Teleport's `typical` predicate library, copied verbatim.
- `internal/set/` - Teleport's `set` package, copied verbatim.
- `cmd/eval/` - command-line evaluator.
- `cmd/wasm/` - WebAssembly entry point exposing `evaluateLabelExpression`.
- `cmd/serve/` - tiny static file server for the web page.
- `web/` - the browser front end (`index.html`, `app.js`, `wasm_exec.js`).

## Command line

```sh
make build
echo 'labels: {env: prod, owner: devs}
username: alice@example.com
traits: {groups: [devs, security]}' |
  ./bin/eval -expr 'contains(user.spec.traits["groups"], labels["owner"])'
# true
```

The input is YAML with three optional top-level keys: `labels` (a string map),
`username` (a string), and `traits` (a map of string lists).

## Web page

```sh
make serve
# open http://localhost:8080
```

The page has two fields, one for the expression and one for the YAML input, two
sample inputs, and an Evaluate button that writes the result below. Evaluation
runs entirely in the browser through the WebAssembly module, so no server-side
evaluation takes place.

## Expression language

The evaluator supports the same variables and functions as Teleport's label
expressions: the `labels` map, `user.metadata.name`, `user.spec.traits`, and the
functions `set`, `contains`, `contains_any`, `contains_all`, `labels_matching`,
`regexp.match`, `regexp.replace`, `email.local`, `strings.upper`, and
`strings.lower`.

## Attribution and license

The contents of `internal/typical`, `internal/set`, and the parser
specification and helper functions in `labelexpr/` are copied, with light edits,
from [gravitational/teleport](https://github.com/gravitational/teleport) at tag
`v18.8.3`. The relevant source files are
[`lib/services/label_expressions.go`](https://github.com/gravitational/teleport/blob/v18.8.3/lib/services/label_expressions.go),
`lib/utils/typical/`, `lib/utils/set/`, `lib/utils/replace.go`, and
`lib/utils/parse/parse.go`.

Because it incorporates Teleport source, this project is licensed under the GNU
Affero General Public License v3.0. See [LICENSE](LICENSE).
