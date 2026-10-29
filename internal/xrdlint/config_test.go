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
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/spf13/afero"
)

const configHeader = "apiVersion: lint.crossplane.io/v1alpha1\nkind: LintConfig\n"

func TestParseConfig(t *testing.T) {
	type want struct {
		rules map[string]RuleConfig
		err   string // substring; empty means no error
	}

	cases := map[string]struct {
		reason string
		data   string
		want   want
	}{
		"Empty": {
			reason: "An empty file is a valid config with no overrides.",
			data:   "",
			want:   want{rules: nil},
		},
		"HeaderOnly": {
			reason: "A config with only its header has no overrides.",
			data:   configHeader,
			want:   want{rules: nil},
		},
		"BareSeverity": {
			reason: "A rule value can be a bare severity.",
			data:   configHeader + "rules:\n  xrd/no-bools: error\n  xrd/lists-need-list-type: off\n",
			want: want{rules: map[string]RuleConfig{
				"xrd/no-bools":             {Severity: SeverityError},
				"xrd/lists-need-list-type": {Severity: SeverityOff},
			}},
		},
		"ObjectForm": {
			reason: "A rule value can be an object with severity and options.",
			data:   configHeader + "rules:\n  xrd/string-max-length: {severity: warn, options: {max: 1024}}\n",
			want: want{rules: map[string]RuleConfig{
				"xrd/string-max-length": {Severity: SeverityWarn, Options: map[string]any{"max": 1024}},
			}},
		},
		"ObjectFormWithoutOptions": {
			reason: "Options are optional in the object form.",
			data:   configHeader + "rules:\n  xrd/no-bools:\n    severity: warn\n",
			want: want{rules: map[string]RuleConfig{
				"xrd/no-bools": {Severity: SeverityWarn},
			}},
		},
		"ListFormFlow": {
			reason: "The ESLint list form is not accepted.",
			data:   configHeader + "rules:\n  xrd/string-max-length: [warn, {max: 1024}]\n",
			want:   want{err: "list form is not supported"},
		},
		"ListFormBlock": {
			reason: "A rule value written as a block sequence is not accepted either.",
			data:   configHeader + "rules:\n  xrd/no-bools:\n    - warn\n",
			want:   want{err: "list form is not supported"},
		},
		"ObjectFormWithoutSeverity": {
			reason: "The object form must set a severity.",
			data:   configHeader + "rules:\n  xrd/no-bools: {options: {}}\n",
			want:   want{err: "severity is required"},
		},
		"ObjectFormUnknownKey": {
			reason: "The object form rejects keys other than severity and options.",
			data:   configHeader + "rules:\n  xrd/no-bools: {severity: warn, level: 2}\n",
			want:   want{err: `unknown key "level"`},
		},
		"UnknownSeverity": {
			reason: "Only off, warn and error are severities.",
			data:   configHeader + "rules:\n  xrd/no-bools: fatal\n",
			want:   want{err: `unknown severity "fatal"`},
		},
		"UnknownTopLevelKey": {
			reason: "Strict parsing rejects an unknown top-level key.",
			data:   configHeader + "rulez:\n  xrd/no-bools: warn\n",
			want:   want{err: "field rulez not found"},
		},
		"DuplicateRule": {
			reason: "The same rule cannot be configured twice in one file.",
			data:   configHeader + "rules:\n  xrd/no-bools: warn\n  xrd/no-bools: error\n",
			want:   want{err: "already defined"},
		},
		"WrongAPIVersion": {
			reason: "The header must name the lint config API.",
			data:   "apiVersion: v1\nkind: LintConfig\n",
			want:   want{err: "apiVersion"},
		},
		"WrongKind": {
			reason: "The header must name the LintConfig kind.",
			data:   "apiVersion: lint.crossplane.io/v1alpha1\nkind: Config\n",
			want:   want{err: "kind"},
		},
		"MissingHeader": {
			reason: "A non-empty config must carry its header.",
			data:   "rules:\n  xrd/no-bools: warn\n",
			want:   want{err: "apiVersion"},
		},
		"CustomRulesNotYetSupported": {
			reason: "Custom rules are not implemented yet, and say so rather than being ignored.",
			data:   configHeader + "customRules:\n  local/x:\n    severity: warn\n",
			want:   want{err: "customRules are not supported yet"},
		},
		"MultipleDocuments": {
			reason: "A config file holds exactly one document.",
			data:   configHeader + "---\n" + configHeader,
			want:   want{err: "one YAML document"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, err := ParseConfig([]byte(tc.data))
			if tc.want.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want.err) {
					t.Fatalf("\n%s\nParseConfig(): want error containing %q, got %v", tc.reason, tc.want.err, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("\n%s\nParseConfig(): unexpected error: %v", tc.reason, err)
			}
			if diff := cmp.Diff(tc.want.rules, cfg.Rules); diff != "" {
				t.Errorf("\n%s\nParseConfig(): -want rules, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	reg, err := NewRegistry(
		fakeRule{meta: Meta{ID: "xrd/a", DefaultSeverity: SeverityError}},
		fakeRule{meta: Meta{ID: "xrd/b", DefaultSeverity: SeverityOff, Options: maxOptionsSchema()}},
	)
	if err != nil {
		t.Fatal(err)
	}

	type want struct {
		settings map[string]Setting
		err      string
	}

	cases := map[string]struct {
		reason string
		cfg    *Config
		want   want
	}{
		"NilConfig": {
			reason: "With no config every rule takes its default severity.",
			cfg:    nil,
			want: want{settings: map[string]Setting{
				"xrd/a": {Severity: SeverityError},
				"xrd/b": {Severity: SeverityOff},
			}},
		},
		"Override": {
			reason: "A config entry overrides a rule's severity and supplies options.",
			cfg: &Config{Rules: map[string]RuleConfig{
				"xrd/a": {Severity: SeverityWarn},
				"xrd/b": {Severity: SeverityError, Options: map[string]any{"max": 5}},
			}},
			want: want{settings: map[string]Setting{
				"xrd/a": {Severity: SeverityWarn},
				"xrd/b": {Severity: SeverityError, Options: []byte(`{"max":5}`)},
			}},
		},
		"UnknownRule": {
			reason: "A rules entry that names no known rule is a config error, so typos surface.",
			cfg:    &Config{Rules: map[string]RuleConfig{"xrd/no-boools": {Severity: SeverityWarn}}},
			want:   want{err: `unknown rule "xrd/no-boools"`},
		},
		"ReservedLintRule": {
			reason: "The linter's own rules cannot be configured.",
			cfg:    &Config{Rules: map[string]RuleConfig{"lint/rule-error": {Severity: SeverityOff}}},
			want:   want{err: `unknown rule "lint/rule-error"`},
		},
		"OptionsOnRuleWithoutOptions": {
			reason: "Options for a rule that takes none are a config error.",
			cfg:    &Config{Rules: map[string]RuleConfig{"xrd/a": {Severity: SeverityWarn, Options: map[string]any{"x": 1}}}},
			want:   want{err: `rule "xrd/a" takes no options`},
		},
		"OptionsFailSchema": {
			reason: "Options are validated against the schema the rule declares.",
			cfg:    &Config{Rules: map[string]RuleConfig{"xrd/b": {Severity: SeverityWarn, Options: map[string]any{"max": "big"}}}},
			want:   want{err: `invalid options for rule "xrd/b"`},
		},
		"OptionsUnknownField": {
			reason: "Options reject fields the rule's schema does not declare.",
			cfg:    &Config{Rules: map[string]RuleConfig{"xrd/b": {Severity: SeverityWarn, Options: map[string]any{"maxx": 3}}}},
			want:   want{err: `invalid options for rule "xrd/b"`},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := tc.cfg.Resolve(reg)
			if tc.want.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want.err) {
					t.Fatalf("\n%s\nResolve(): want error containing %q, got %v", tc.reason, tc.want.err, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("\n%s\nResolve(): unexpected error: %v", tc.reason, err)
			}
			if diff := cmp.Diff(tc.want.settings, got); diff != "" {
				t.Errorf("\n%s\nResolve(): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestFindConfig(t *testing.T) {
	cases := map[string]struct {
		reason string
		files  []string
		start  string
		want   string
	}{
		"InStartDir": {
			reason: "A config in the start directory is found.",
			files:  []string{"/repo/apis/.crossplane-lint.yaml"},
			start:  "/repo/apis",
			want:   "/repo/apis/.crossplane-lint.yaml",
		},
		"InAncestor": {
			reason: "The search walks upward to an ancestor directory.",
			files:  []string{"/repo/.crossplane-lint.yaml"},
			start:  "/repo/apis/network",
			want:   "/repo/.crossplane-lint.yaml",
		},
		"NearestWins": {
			reason: "The nearest config wins over one further up.",
			files:  []string{"/repo/.crossplane-lint.yaml", "/repo/apis/.crossplane-lint.yaml"},
			start:  "/repo/apis/network",
			want:   "/repo/apis/.crossplane-lint.yaml",
		},
		"None": {
			reason: "With no config anywhere the result is empty.",
			start:  "/repo/apis",
			want:   "",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			for _, f := range tc.files {
				if err := afero.WriteFile(fs, f, []byte(configHeader), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := fs.MkdirAll(tc.start, 0o755); err != nil {
				t.Fatal(err)
			}

			got, err := FindConfig(fs, tc.start)
			if err != nil {
				t.Fatalf("\n%s\nFindConfig(): unexpected error: %v", tc.reason, err)
			}
			if got != tc.want {
				t.Errorf("\n%s\nFindConfig(): want %q, got %q", tc.reason, tc.want, got)
			}
		})
	}
}
