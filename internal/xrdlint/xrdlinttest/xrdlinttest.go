/*
Copyright 2026 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package xrdlinttest runs table-driven tests of xrdlint rules, in the spirit
// of ESLint's RuleTester.
package xrdlinttest

import (
	"sort"
	"strings"
	"testing"

	"github.com/crossplane/cli/v2/internal/xrdlint"
)

// header is a minimal XRD with one served version. XRD appends a schema to
// it.
const header = `apiVersion: apiextensions.crossplane.io/v2
kind: CompositeResourceDefinition
metadata:
  name: xtests.example.org
spec:
  group: example.org
  names:
    kind: XTest
    plural: xtests
  versions:
  - name: v1alpha1
    served: true
    referenceable: true
    schema:
      openAPIV3Schema:
`

// XRD returns a minimal XRD whose single served version has the supplied
// openAPIV3Schema, written unindented. A leading newline is ignored.
func XRD(schema string) string {
	var b strings.Builder
	b.WriteString(header)
	for l := range strings.SplitSeq(strings.TrimPrefix(schema, "\n"), "\n") {
		if l == "" {
			continue
		}
		b.WriteString("        ")
		b.WriteString(l)
		b.WriteString("\n")
	}
	return b.String()
}

// A Case is one rule test.
type Case struct {
	// Reason explains what the case shows.
	Reason string

	// XRD is the YAML the rule runs on.
	XRD string

	// Options are the rule's options, if any.
	Options map[string]any

	// Want are the findings the rule should produce, in order. An empty Want
	// means the XRD passes.
	Want []Want
}

// Want is an expected finding.
type Want struct {
	// Path is the finding's field path.
	Path string

	// Message, if set, must equal the finding's message.
	Message string
}

// Run runs each case against the rule, enabled at its default severity, or
// at error if it is off by default. It requires at least one passing and one
// failing case, and fails on any finding the linter reports itself, such as
// lint/rule-error.
func Run(t *testing.T, rule xrdlint.SchemaRule, cases map[string]Case) {
	t.Helper()

	id := rule.Meta().ID
	var passing, failing bool
	names := make([]string, 0, len(cases))
	for name, tc := range cases {
		names = append(names, name)
		if len(tc.Want) == 0 {
			passing = true
		} else {
			failing = true
		}
	}
	sort.Strings(names)
	if !passing || !failing {
		t.Errorf("%s: need at least one passing and one failing case", id)
	}

	for _, name := range names {
		tc := cases[name]
		t.Run(name, func(t *testing.T) {
			t.Helper()
			got := run(t, rule, tc)

			if len(got) != len(tc.Want) {
				t.Fatalf("\n%s\n%s: want %d findings, got %d:\n%s", tc.Reason, id, len(tc.Want), len(got), describe(got))
			}
			for i, w := range tc.Want {
				if got[i].Path != w.Path {
					t.Errorf("\n%s\n%s: finding %d: want path %q, got %q", tc.Reason, id, i, w.Path, got[i].Path)
				}
				if w.Message != "" && got[i].Message != w.Message {
					t.Errorf("\n%s\n%s: finding %d: want message %q, got %q", tc.Reason, id, i, w.Message, got[i].Message)
				}
			}
		})
	}
}

func run(t *testing.T, rule xrdlint.SchemaRule, tc Case) []xrdlint.Finding {
	t.Helper()

	m := rule.Meta()
	reg, err := xrdlint.NewRegistry(rule)
	if err != nil {
		t.Fatalf("NewRegistry(): %v", err)
	}
	sev := m.DefaultSeverity
	if sev == xrdlint.SeverityOff || sev == "" {
		sev = xrdlint.SeverityError
	}
	cfg := &xrdlint.Config{Rules: map[string]xrdlint.RuleConfig{m.ID: {Severity: sev, Options: tc.Options}}}
	settings, err := cfg.Resolve(reg)
	if err != nil {
		t.Fatalf("\n%s\nResolve(): %v", tc.Reason, err)
	}
	xrds, err := xrdlint.Parse("xrd.yaml", []byte(tc.XRD))
	if err != nil {
		t.Fatalf("\n%s\nParse(): %v", tc.Reason, err)
	}
	if len(xrds) == 0 {
		t.Fatalf("\n%s\nParse(): no XRD in the case", tc.Reason)
	}

	findings := xrdlint.NewRunner(reg, settings).Run(xrds).Findings
	out := make([]xrdlint.Finding, 0, len(findings))
	for _, f := range findings {
		if f.Rule != m.ID {
			t.Fatalf("\n%s\nunexpected finding from %s: %s", tc.Reason, f.Rule, f.Message)
		}
		out = append(out, f)
	}
	return out
}

func describe(fs []xrdlint.Finding) string {
	var b strings.Builder
	for _, f := range fs {
		b.WriteString("  " + f.Path + ": " + f.Message + "\n")
	}
	return b.String()
}
