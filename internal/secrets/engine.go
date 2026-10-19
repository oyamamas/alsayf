package secrets

import (
	"errors"
	"sort"
	"sync/atomic"

	"github.com/praetorian-inc/titus/pkg/matcher"
	"github.com/praetorian-inc/titus/pkg/rule"
	"github.com/praetorian-inc/titus/pkg/types"
)

// Keep upstream match types and raw secret material inside this module.
type detection struct {
	ruleID, ruleName string
	groups           [][]byte
	start, end       int64
}

type engine interface {
	scan([]byte) ([]detection, error)
	close() error
}

type titusEngine struct {
	matcher matcher.Matcher
	warned  atomic.Bool
}

func builtinRules() ([]*types.Rule, error) {
	rules, err := rule.NewLoader().LoadBuiltinRules()
	if err != nil {
		return nil, errors.New("cannot load embedded detection rules")
	}
	// Noisy generic rules and non-secret asset/hash rules are excluded.
	var selected []*types.Rule
	for _, r := range rules {
		for _, category := range r.Categories {
			if category == "secret" && !r.Noisy {
				selected = append(selected, r)
				break
			}
		}
	}
	return selected, nil
}

func newEngine() (engine, error) {
	rules, err := builtinRules()
	if err != nil {
		return nil, err
	}
	e := &titusEngine{}
	// The top-level Titus API imports online validators and scoring clients.
	// Import only the local matcher and embedded rule loader instead.
	e.matcher, err = matcher.New(matcher.Config{
		Rules:        rules,
		ContextLines: 0,
		// Upstream diagnostics may contain input data. Never forward them.
		WarnFunc: func(string, ...any) { e.warned.Store(true) },
	})
	if err != nil {
		return nil, errors.New("cannot initialize local detection engine")
	}
	return e, nil
}

func (e *titusEngine) scan(content []byte) ([]detection, error) {
	e.warned.Store(false)
	matches, err := e.matcher.Match(content)
	if err != nil {
		return nil, errors.New("local pattern matching failed")
	}
	// Drain per file, so retries cannot retain many files or lose provenance.
	retried, retryErr := e.matcher.DrainTimedOut()
	matches = append(matches, retried...)
	out := make([]detection, 0, len(matches))
	for _, m := range matches {
		groups := m.Groups
		if len(groups) == 0 {
			groups = [][]byte{m.Snippet.Matching}
		}
		out = append(out, detection{m.RuleID, m.RuleName, groups, m.Location.Offset.Start, m.Location.Offset.End})
	}
	if retryErr != nil || e.warned.Load() {
		return out, errors.New("some detection rules failed or timed out; results may be incomplete")
	}
	return out, nil
}

func (e *titusEngine) close() error { return e.matcher.Close() }

func Rules() ([]Rule, error) {
	rules, err := builtinRules()
	if err != nil {
		return nil, err
	}
	out := make([]Rule, 0, len(rules))
	for _, r := range rules {
		out = append(out, Rule{r.ID, r.Name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
