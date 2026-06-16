package resourcematcher

import "github.com/gravitational/trace"

// DesugaredRule lowers a declarative rule to the bare predicate form for
// display, preserving the audit metadata. A deny hint with no On has its On
// materialized to the rule's path-and-method territory, since the bare form has
// no path or method clause to default from. A rule already in the predicate
// form is returned unchanged. This is a demo-only helper, kept in its own file
// so the engine files synced from upstream stay identical, and lets the web
// tool show a declarative rule's bare form.
func (r Rule) DesugaredRule() (Rule, error) {
	if r.Pred != "" {
		return r, nil
	}
	pred, err := r.desugar()
	if err != nil {
		return Rule{}, trace.Wrap(err)
	}
	defaultOn, err := r.defaultHintOn()
	if err != nil {
		return Rule{}, trace.Wrap(err)
	}
	var hints []DenyHint
	for _, h := range r.DenyHints {
		on := h.On
		if on == "" {
			on = defaultOn
		}
		hints = append(hints, DenyHint{On: on, DenyCode: h.DenyCode, DenyReason: h.DenyReason})
	}
	return Rule{
		Where:       pred,
		AllowCode:   r.AllowCode,
		AllowReason: r.AllowReason,
		DenyHints:   hints,
		URLDecoding: r.URLDecoding,
	}, nil
}
