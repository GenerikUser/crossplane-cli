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

package xrdlint

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

const boolsSchema = `        type: object
        properties:
          spec:
            type: object
            properties:
              public:
                type: boolean
              private:
                type: boolean
`

func isBool(p Property) bool { return p.Schema.Type == "boolean" }

func TestRun(t *testing.T) {
	boolRule := fakeRule{
		meta:  Meta{ID: "xrd/bools", DefaultSeverity: SeverityWarn, DocsURL: "https://example.org/bools"},
		check: reportWhere(isBool, "prefer an enum"),
	}

	// Only the fields a case cares about are compared.
	type got struct {
		Rule     string
		Severity Severity
		Line     int
		Path     string
	}

	cases := map[string]struct {
		reason   string
		rules    []SchemaRule
		cfg      *Config
		data     string
		want     []got
		wantFail bool
	}{
		"DefaultSeverity": {
			reason: "A rule reports at its default severity, one finding per property, in line order.",
			rules:  []SchemaRule{boolRule},
			data:   withSchema(boolsSchema),
			want: []got{
				{Rule: "xrd/bools", Severity: SeverityWarn, Line: 21, Path: "spec.public"},
				{Rule: "xrd/bools", Severity: SeverityWarn, Line: 23, Path: "spec.private"},
			},
		},
		"SeverityOverride": {
			reason: "A config override changes the severity of the findings.",
			rules:  []SchemaRule{boolRule},
			cfg:    &Config{Rules: map[string]RuleConfig{"xrd/bools": {Severity: SeverityError}}},
			data:   withSchema(boolsSchema),
			want: []got{
				{Rule: "xrd/bools", Severity: SeverityError, Line: 21, Path: "spec.public"},
				{Rule: "xrd/bools", Severity: SeverityError, Line: 23, Path: "spec.private"},
			},
			wantFail: true,
		},
		"Off": {
			reason: "A rule turned off does not run.",
			rules: []SchemaRule{fakeRule{
				meta:  Meta{ID: "xrd/bools", DefaultSeverity: SeverityError},
				check: failing("must not run"),
			}},
			cfg:  &Config{Rules: map[string]RuleConfig{"xrd/bools": {Severity: SeverityOff}}},
			data: withSchema(boolsSchema),
			want: nil,
		},
		"RuleErrorFailsRun": {
			reason: "A warn rule that fails to run produces an unsuppressible lint/rule-error, and the other rules still report.",
			rules: []SchemaRule{
				boolRule,
				fakeRule{meta: Meta{ID: "xrd/broken", DefaultSeverity: SeverityWarn}, check: failing("boom")},
			},
			data: "# crossplane-lint-disable-next-line lint/rule-error\n" + withSchema(`        type: object
        properties:
          spec:
            type: object
            properties:
              public:
                type: boolean
`),
			want: []got{
				{Rule: "lint/unused-disable", Severity: SeverityWarn, Line: 1},
				{Rule: "lint/rule-error", Severity: SeverityError, Line: 2},
				{Rule: "xrd/bools", Severity: SeverityWarn, Line: 22, Path: "spec.public"},
			},
			wantFail: true,
		},
		"RulePanicIsRuleError": {
			reason: "A rule that panics is reported as lint/rule-error rather than crashing the run.",
			rules: []SchemaRule{fakeRule{
				meta:  Meta{ID: "xrd/panics", DefaultSeverity: SeverityWarn},
				check: func(*Context, *XRD) error { panic("oops") },
			}},
			data:     withSchema(boolsSchema),
			want:     []got{{Rule: "lint/rule-error", Severity: SeverityError, Line: 1}},
			wantFail: true,
		},
		"InvalidXRDSkipsRules": {
			reason: "An XRD that is not structural gets lint/invalid-xrd and no other rule runs on it.",
			rules:  []SchemaRule{fakeRule{meta: Meta{ID: "xrd/x", DefaultSeverity: SeverityError}, check: failing("must not run")}},
			data: withSchema(`        type: object
        properties:
          spec:
            type: object
            properties:
              acl: {}
`),
			want:     []got{{Rule: "lint/invalid-xrd", Severity: SeverityError, Line: 1}},
			wantFail: true,
		},
		"SuppressNodeAndChildren": {
			reason: "A disable comment suppresses the named rule on the next node and everything beneath it.",
			rules:  []SchemaRule{boolRule},
			data: xrdHeader + `        type: object
        properties:
          # crossplane-lint-disable-next-line xrd/bools -- kept for v1 compatibility
          spec:
            type: object
            properties:
              public:
                type: boolean
`,
			want: nil,
		},
		"SuppressOneOfSeveral": {
			reason: "A disable comment only covers its own node; siblings are still reported, and a named rule that suppressed nothing is reported too.",
			rules:  []SchemaRule{boolRule},
			data: xrdHeader + `        type: object
        properties:
          spec:
            type: object
            properties:
              # crossplane-lint-disable-next-line xrd/other, xrd/bools
              public:
                type: boolean
              private:
                type: boolean
`,
			want: []got{
				{Rule: "lint/unused-disable", Severity: SeverityWarn, Line: 21},
				{Rule: "xrd/bools", Severity: SeverityWarn, Line: 24, Path: "spec.private"},
			},
		},
		"UnusedDisable": {
			reason: "A disable comment that suppresses nothing is reported.",
			rules:  []SchemaRule{boolRule},
			data: xrdHeader + `        type: object
        properties:
          spec:
            # crossplane-lint-disable-next-line xrd/bools
            type: object
`,
			want: []got{{Rule: "lint/unused-disable", Severity: SeverityWarn, Line: 19}},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			reg, err := NewRegistry(tc.rules...)
			if err != nil {
				t.Fatal(err)
			}
			settings, err := tc.cfg.Resolve(reg)
			if err != nil {
				t.Fatal(err)
			}
			xrds, err := Parse("xrd.yaml", []byte(tc.data))
			if err != nil {
				t.Fatal(err)
			}

			res := NewRunner(reg, settings).Run(xrds)

			gotFindings := make([]got, 0, len(res.Findings))
			for _, f := range res.Findings {
				gotFindings = append(gotFindings, got{Rule: f.Rule, Severity: f.Severity, Line: f.Line, Path: f.Path})
				if f.File != "xrd.yaml" || f.XRD != "xnetworks.example.org" || f.Message == "" {
					t.Errorf("\n%s\nRun(): finding is missing file, XRD or message: %+v", tc.reason, f)
				}
			}
			if diff := cmp.Diff(tc.want, gotFindings, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("\n%s\nRun(): -want, +got:\n%s", tc.reason, diff)
			}
			if gotFail := res.Failed(-1); gotFail != tc.wantFail {
				t.Errorf("\n%s\nFailed(-1): want %t, got %t", tc.reason, tc.wantFail, gotFail)
			}
		})
	}
}

func TestRunFindingDetail(t *testing.T) {
	reg, err := NewRegistry(fakeRule{
		meta:  Meta{ID: "xrd/bools", DefaultSeverity: SeverityWarn, DocsURL: "https://example.org/bools"},
		check: reportWhere(isBool, "prefer an enum"),
	})
	if err != nil {
		t.Fatal(err)
	}
	settings, _ := (*Config)(nil).Resolve(reg)
	res := NewRunner(reg, settings).Run([]*XRD{parseOne(t, withSchema(`        type: object
        properties:
          spec:
            type: object
            properties:
              public:
                type: boolean
`))})

	want := []Finding{{
		Rule:     "xrd/bools",
		Severity: SeverityWarn,
		File:     "xrd.yaml",
		Line:     21,
		Column:   15,
		XRD:      "xnetworks.example.org",
		Version:  "v1alpha1",
		Path:     "spec.public",
		Message:  "prefer an enum",
		DocsURL:  "https://example.org/bools",
	}}
	if diff := cmp.Diff(want, res.Findings); diff != "" {
		t.Errorf("Run(): -want, +got:\n%s", diff)
	}
	if diff := cmp.Diff(Summary{Warnings: 1}, res.Summary); diff != "" {
		t.Errorf("Run(): summary -want, +got:\n%s", diff)
	}
}

func TestFailed(t *testing.T) {
	cases := map[string]struct {
		reason      string
		summary     Summary
		maxWarnings int
		want        bool
	}{
		"Clean":             {reason: "No findings pass.", want: false, maxWarnings: -1},
		"Error":             {reason: "Any error fails.", summary: Summary{Errors: 1}, maxWarnings: -1, want: true},
		"WarningsUnlimited": {reason: "Warnings pass with no budget.", summary: Summary{Warnings: 9}, maxWarnings: -1, want: false},
		"WithinBudget":      {reason: "Warnings within the budget pass.", summary: Summary{Warnings: 2}, maxWarnings: 2, want: false},
		"OverBudget":        {reason: "Warnings over the budget fail.", summary: Summary{Warnings: 3}, maxWarnings: 2, want: true},
		"ZeroBudget":        {reason: "A zero budget fails on any warning.", summary: Summary{Warnings: 1}, maxWarnings: 0, want: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := &Result{Summary: tc.summary}
			if got := r.Failed(tc.maxWarnings); got != tc.want {
				t.Errorf("\n%s\nFailed(%d): want %t, got %t", tc.reason, tc.maxWarnings, tc.want, got)
			}
		})
	}
}

func TestNewRegistry(t *testing.T) {
	cases := map[string]struct {
		reason string
		rules  []SchemaRule
		ok     bool
	}{
		"Valid":           {reason: "Distinct, well-formed IDs register.", rules: []SchemaRule{fakeRule{meta: Meta{ID: "xrd/a"}}, fakeRule{meta: Meta{ID: "xrd/b-c"}}}, ok: true},
		"Duplicate":       {reason: "Two rules cannot share an ID.", rules: []SchemaRule{fakeRule{meta: Meta{ID: "xrd/a"}}, fakeRule{meta: Meta{ID: "xrd/a"}}}},
		"NoPrefix":        {reason: "An ID needs a prefix.", rules: []SchemaRule{fakeRule{meta: Meta{ID: "a"}}}},
		"NotKebab":        {reason: "An ID is lower-case kebab case.", rules: []SchemaRule{fakeRule{meta: Meta{ID: "xrd/noBools"}}}},
		"ReservedLint":    {reason: "The lint/ prefix is reserved for the linter.", rules: []SchemaRule{fakeRule{meta: Meta{ID: "lint/x"}}}},
		"InvalidSeverity": {reason: "A default severity must be a known severity.", rules: []SchemaRule{fakeRule{meta: Meta{ID: "xrd/a", DefaultSeverity: "fatal"}}}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := NewRegistry(tc.rules...)
			if (err == nil) != tc.ok {
				t.Errorf("\n%s\nNewRegistry(): want ok=%t, got err=%v", tc.reason, tc.ok, err)
			}
		})
	}
}
