// Command gengoldens converts the resourcematcher golden testdata into a JSON
// file the web playground can load, in the same schema as the hand-curated
// web/app-access/samples.json: a list of {topic, examples:[{name, rule,
// input}]}. Each golden file under internal/resourcematcher/testdata becomes a
// group of examples, one per case, so the worked examples that pin the engine's
// behaviour are also browsable in the playground without hand-copying them.
//
// The output is written to a separate file (web/app-access/goldens.json by
// default) so it never clobbers the curated samples. Regenerate it with
// "make goldens" after changing the golden testdata.
//
// Usage:
//
//	gengoldens [-testdata <dir>] [-out <file>]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	rm "github.com/juliaogris/teleport-expressions/internal/resourcematcher"
)

// identity is the caller identity in golden form, matching the YAML the golden
// files and the playground input share.
type identity struct {
	Name   string              `yaml:"name,omitempty"`
	Roles  []string            `yaml:"roles,omitempty"`
	Traits map[string][]string `yaml:"traits,omitempty"`
}

// request is the HTTP request in golden form.
type request struct {
	Method string `yaml:"method"`
	Path   string `yaml:"path"`
}

// goldenFile is the subset of a golden testdata file this command reads. It
// ignores the generated rules_desugared and expect blocks, since the playground
// evaluates live and shows its own result.
type goldenFile struct {
	Description string    `yaml:"description"`
	Rules       []rm.Rule `yaml:"rules"`
	Identity    *identity `yaml:"identity"`
	Cases       []struct {
		Request  request   `yaml:"request"`
		Identity *identity `yaml:"identity"`
	} `yaml:"cases"`
	Error string `yaml:"error"`
}

// resourcesDoc is the role wrapper the playground rule field carries: a role
// name and its app_resources, the exact shape appaccess.Evaluate parses.
type resourcesDoc struct {
	RoleName     string    `yaml:"role_name"`
	AppResources []rm.Rule `yaml:"app_resources"`
}

// playgroundInput is the request-and-identity YAML the playground input field
// carries.
type playgroundInput struct {
	Request  request  `yaml:"request"`
	Identity identity `yaml:"identity"`
}

// example is one playground example: a name, a rule YAML string, and an input
// YAML string.
type example struct {
	Name  string `json:"name"`
	Rule  string `json:"rule"`
	Input string `json:"input"`
}

// topic groups the examples lowered from one testdata directory.
type topic struct {
	Topic    string    `json:"topic"`
	Examples []example `json:"examples"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	testdata := flag.String("testdata", "internal/resourcematcher/testdata", "golden testdata directory")
	out := flag.String("out", "web/app-access/goldens.json", "output JSON file")
	flag.Parse()

	// Collect files grouped by their directory, so each testdata subdir becomes
	// one topic in directory order and each file's cases keep their order.
	byTopic := map[string][]example{}
	var order []string
	err := filepath.WalkDir(*testdata, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var g goldenFile
		if err := yaml.Unmarshal(raw, &g); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		examples, err := examplesFromGolden(g)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		name := topicName(filepath.Dir(path), *testdata)
		if _, seen := byTopic[name]; !seen {
			order = append(order, name)
		}
		byTopic[name] = append(byTopic[name], examples...)
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(order)

	topics := make([]topic, 0, len(order))
	for _, name := range order {
		topics = append(topics, topic{Topic: name, Examples: byTopic[name]})
	}

	encoded, err := json.MarshalIndent(topics, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(*out, encoded, 0o644)
}

// examplesFromGolden lowers one golden file to playground examples. A file with
// cases yields one example per case; a file that only asserts a load error (no
// cases) yields a single example with an empty input, since the rule fails
// before any request is evaluated.
func examplesFromGolden(g goldenFile) ([]example, error) {
	if len(g.Cases) == 0 {
		rule, err := ruleYAML(g)
		if err != nil {
			return nil, err
		}
		return []example{{Name: g.Description, Rule: rule, Input: ""}}, nil
	}
	rule, err := ruleYAML(g)
	if err != nil {
		return nil, err
	}
	out := make([]example, 0, len(g.Cases))
	for _, c := range g.Cases {
		id := g.Identity
		if c.Identity != nil {
			id = c.Identity
		}
		input, err := inputYAML(c.Request, id)
		if err != nil {
			return nil, err
		}
		out = append(out, example{Name: exampleName(g, c.Request), Rule: rule, Input: input})
	}
	return out, nil
}

// ruleYAML renders the golden rules as the role-wrapped app_resources YAML the
// playground rule field expects, choosing the identity's first role as the role
// name, or "developer" when none is set.
func ruleYAML(g goldenFile) (string, error) {
	role := "developer"
	if g.Identity != nil && len(g.Identity.Roles) > 0 {
		role = g.Identity.Roles[0]
	}
	doc := resourcesDoc{RoleName: role, AppResources: g.Rules}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// inputYAML renders one request and identity as the playground input YAML.
func inputYAML(req request, id *identity) (string, error) {
	in := playgroundInput{Request: req}
	if id != nil {
		in.Identity = *id
	}
	out, err := yaml.Marshal(in)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// exampleName names an example by the file description and, when the file has
// more than one case, the request that distinguishes this one.
func exampleName(g goldenFile, req request) string {
	if len(g.Cases) <= 1 {
		return g.Description
	}
	return fmt.Sprintf("%s (%s %s)", g.Description, req.Method, req.Path)
}

// topicName derives a readable topic from a testdata subdirectory: the path
// relative to the testdata root, with a leading "NN_" ordering prefix dropped
// and underscores turned into spaces.
func topicName(dir, testdata string) string {
	rel, err := filepath.Rel(testdata, dir)
	if err != nil || rel == "." {
		rel = "general"
	}
	base := filepath.Base(rel)
	if i := strings.IndexByte(base, '_'); i >= 0 && allDigits(base[:i]) {
		base = base[i+1:]
	}
	return strings.ReplaceAll(base, "_", " ")
}

// allDigits reports whether s is a non-empty run of ASCII digits, the form of a
// testdata directory's ordering prefix.
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
