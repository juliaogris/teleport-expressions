// Command gengoldens generates the web playground's example file,
// web/app-access/samples.json, from the resourcematcher golden testdata. Each
// golden file under internal/resourcematcher/testdata becomes one example,
// taken from the file's first case, so the playground stays at curated scale
// and every example it shows is an example the golden tests already verify. The
// rules are lifted from the raw testdata as a yaml.Node, so the author's
// explanatory comments survive into the playground.
//
// A hand-curated overlay (web/app-access/overlay.json, same schema) is merged
// after the generated topics. It carries the web-only examples a single golden
// file cannot express, such as a multi-role scenario or the hand-authored tour.
//
// The testdata is the single source of truth: edit a golden file (its
// description names the example, its first case is the one shown) and rerun
// "make samples".
//
// Usage:
//
//	gengoldens [-testdata <dir>] [-overlay <file>] [-out <file>]
package main

import (
	"bytes"
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

// playgroundInput is the request-and-identity YAML the playground input field
// carries.
type playgroundInput struct {
	Request  request  `yaml:"request"`
	Identity identity `yaml:"identity"`
}

// example is one playground example: a name, a rule YAML string, and an input
// YAML string. In an overlay an example may instead carry From, the testdata
// file it pulls its rule and input from, so a curated topic such as the tour
// reuses a verified example rather than duplicating its rule text. An optional
// Name then overrides the source file's description for the curated framing.
type example struct {
	Name  string `json:"name"`
	Rule  string `json:"rule"`
	Input string `json:"input"`
	From  string `json:"from,omitempty"`
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
	out := flag.String("out", "web/app-access/samples.json", "output JSON file")
	overlay := flag.String("overlay", "web/app-access/overlay.json", "web-only overlay JSON merged after the generated topics, empty to skip")
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
		examples, err := examplesFromFile(path)
		if err != nil {
			return err
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

	if *overlay != "" {
		topics, err = mergeOverlay(topics, *overlay, *testdata)
		if err != nil {
			return err
		}
	}

	topics = applyTopicOrder(topics)

	encoded, err := json.MarshalIndent(topics, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(*out, encoded, 0o644)
}

// mergeOverlay folds the hand-curated web-only topics into the generated set.
// The overlay carries examples a golden file cannot express, such as a
// multi-role scenario or a hand-authored tour, in the same {topic, examples}
// schema. An overlay topic whose name matches a generated topic appends its
// examples to that topic; a new name is added as its own topic. The final order
// is set by applyTopicOrder, so the overlay need not be positioned. An overlay
// example that sets From pulls its rule and input from that testdata file rather
// than carrying them inline, so a curated topic reuses a verified example; an
// inline Name then overrides the source's description.
func mergeOverlay(topics []topic, path, testdata string) ([]topic, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var extra []topic
	if err := json.Unmarshal(raw, &extra); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	index := map[string]int{}
	for i, t := range topics {
		index[t.Topic] = i
	}
	for _, t := range extra {
		resolved, err := resolveOverlayExamples(t.Examples, testdata)
		if err != nil {
			return nil, fmt.Errorf("overlay topic %q: %w", t.Topic, err)
		}
		if i, ok := index[t.Topic]; ok {
			topics[i].Examples = append(topics[i].Examples, resolved...)
			continue
		}
		index[t.Topic] = len(topics)
		topics = append(topics, topic{Topic: t.Topic, Examples: resolved})
	}
	return topics, nil
}

// resolveOverlayExamples turns each overlay example into a concrete one. An
// example with From is loaded from that testdata file, taking its single
// example and applying an optional Name override; an inline example passes
// through unchanged.
func resolveOverlayExamples(in []example, testdata string) ([]example, error) {
	out := make([]example, 0, len(in))
	for _, e := range in {
		if e.From == "" {
			out = append(out, e)
			continue
		}
		from, err := examplesFromFile(filepath.Join(testdata, e.From))
		if err != nil {
			return nil, err
		}
		if len(from) == 0 {
			return nil, fmt.Errorf("%s yielded no example", e.From)
		}
		ex := from[0]
		if e.Name != "" {
			ex.Name = e.Name
		}
		ex.From = ""
		out = append(out, ex)
	}
	return out, nil
}

// topicOrder is the playground's topic display order. A topic not listed sorts
// after the listed ones, keeping its generated position.
var topicOrder = []string{
	"tour",
	"path",
	"globs",
	"method",
	"capture",
	"trailing slash",
	"combination",
	"carve-outs",
	"root paths",
	"encoded slash",
	"allow / deny codes",
	"roles",
}

// topicDisplay maps a directory-derived topic name to its playground display
// name, for names a directory cannot carry (a "/") or that read better
// hyphenated. The generated name is the map key, the displayed name its value.
var topicDisplay = map[string]string{
	"carve outs":       "carve-outs",
	"allow deny codes": "allow / deny codes",
}

// applyTopicOrder renames topics to their display names and sorts them into the
// playground order. It is a stable sort, so two topics with the same rank, such
// as any not listed in topicOrder, keep their incoming order.
func applyTopicOrder(topics []topic) []topic {
	for i := range topics {
		if name, ok := topicDisplay[topics[i].Topic]; ok {
			topics[i].Topic = name
		}
	}
	rank := map[string]int{}
	for i, name := range topicOrder {
		rank[name] = i
	}
	rankOf := func(name string) int {
		if r, ok := rank[name]; ok {
			return r
		}
		return len(topicOrder)
	}
	sort.SliceStable(topics, func(i, j int) bool {
		return rankOf(topics[i].Topic) < rankOf(topics[j].Topic)
	})
	return topics
}

// examplesFromFile reads one golden testdata file and lowers it to playground
// examples.
func examplesFromFile(path string) ([]example, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var g goldenFile
	if err := yaml.Unmarshal(raw, &g); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	examples, err := examplesFromGolden(g, raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return examples, nil
}

// examplesFromGolden lowers one golden file to playground examples. A file with
// cases yields one example per case; a file that only asserts a load error (no
// cases) yields a single example with an empty input, since the rule fails
// before any request is evaluated.
//
// A file with cases yields one example from its first case, the headline the
// author leads with. The remaining cases are extra coverage for the golden
// tests, not separate playground entries, so a file maps to one example and the
// playground stays at curated scale rather than expanding to every case. To
// show a different request, the playground is interactive: edit the input.
func examplesFromGolden(g goldenFile, raw []byte) ([]example, error) {
	rule, err := ruleYAML(g, raw)
	if err != nil {
		return nil, err
	}
	if len(g.Cases) == 0 {
		return []example{{Name: g.Description, Rule: rule, Input: ""}}, nil
	}
	c := g.Cases[0]
	id := g.Identity
	if c.Identity != nil {
		id = c.Identity
	}
	input, err := inputYAML(c.Request, id)
	if err != nil {
		return nil, err
	}
	return []example{{Name: g.Description, Rule: rule, Input: input}}, nil
}

// ruleYAML renders the golden rules as the role-wrapped app_resources YAML the
// playground rule field expects, choosing the identity's first role as the role
// name, or "developer" when none is set. It lifts the rules straight out of the
// raw testdata document as a yaml.Node rather than re-marshaling the parsed
// struct, so the author's explanatory comments survive into the playground. The
// rules node is re-emitted under app_resources at the playground's two-space
// indent.
func ruleYAML(g goldenFile, raw []byte) (string, error) {
	role := "developer"
	if g.Identity != nil && len(g.Identity.Roles) > 0 {
		role = g.Identity.Roles[0]
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return "", err
	}
	if len(doc.Content) == 0 {
		return "", fmt.Errorf("empty document")
	}
	rules := mapValue(doc.Content[0], "rules")
	if rules == nil {
		return "", fmt.Errorf("no rules key")
	}
	wrapped := &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "role_name"},
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: role},
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "app_resources"},
			rules,
		},
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(wrapped); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

// mapValue returns the value node for key in a YAML mapping node, or nil.
func mapValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
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
