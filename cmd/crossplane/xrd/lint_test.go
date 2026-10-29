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

package xrd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/spf13/afero"

	"github.com/crossplane/cli/v2/internal/xrdlint"
	"github.com/crossplane/cli/v2/internal/xrdlint/xrdlinttest"
)

const lintConfigHeader = "apiVersion: lint.crossplane.io/v1alpha1\nkind: LintConfig\n"

// lintCleanXRD passes every built-in rule.
var lintCleanXRD = xrdlinttest.XRD(`
type: object
properties:
  spec:
    type: object
    properties:
      zones:
        type: array
        x-kubernetes-list-type: atomic
        items:
          type: string
`)

// lintBoolXRD has one boolean, which warns by default.
var lintBoolXRD = xrdlinttest.XRD(`
type: object
properties:
  spec:
    type: object
    properties:
      public:
        type: boolean
`)

// lintListXRD has an array with no list type, which is an error by default.
var lintListXRD = xrdlinttest.XRD(`
type: object
properties:
  spec:
    type: object
    properties:
      zones:
        type: array
        items:
          type: string
`)

func TestLintRun(t *testing.T) {
	type want struct {
		err    string   // substring of the returned error; empty means success
		output []string // substrings of the output
	}

	cases := map[string]struct {
		reason string
		files  map[string]string
		cmd    lintCmd
		stdin  string
		want   want
	}{
		"Clean": {
			reason: "An XRD with no findings passes.",
			files:  map[string]string{"/repo/apis/xrd.yaml": lintCleanXRD},
			cmd:    lintCmd{Paths: []string{"/repo/apis"}, MaxWarnings: -1},
			want:   want{output: []string{"0 errors, 0 warnings"}},
		},
		"WarningPasses": {
			reason: "A warning is reported without failing the run.",
			files:  map[string]string{"/repo/apis/xrd.yaml": lintBoolXRD},
			cmd:    lintCmd{Paths: []string{"/repo/apis"}, MaxWarnings: -1},
			want:   want{output: []string{"spec.public: prefer an enum to a boolean", "xrd/no-bools", "0 errors, 1 warning"}},
		},
		"WarningBudget": {
			reason: "Warnings beyond --max-warnings fail the run.",
			files:  map[string]string{"/repo/apis/xrd.yaml": lintBoolXRD},
			cmd:    lintCmd{Paths: []string{"/repo/apis"}, MaxWarnings: 0},
			want:   want{err: "1 warning exceeds the maximum of 0", output: []string{"0 errors, 1 warning"}},
		},
		"ErrorFails": {
			reason: "An error finding fails the run.",
			files:  map[string]string{"/repo/apis/xrd.yaml": lintListXRD},
			cmd:    lintCmd{Paths: []string{"/repo/apis/xrd.yaml"}, MaxWarnings: -1},
			want:   want{err: "lint found 1 error", output: []string{"xrd/lists-need-list-type", "1 error, 0 warnings"}},
		},
		"ConfigDiscovered": {
			reason: "A config found upward from the working directory overrides a default.",
			files: map[string]string{
				"/repo/.crossplane-lint.yaml": lintConfigHeader + "rules:\n  xrd/no-bools: error\n",
				"/repo/apis/xrd.yaml":         lintBoolXRD,
			},
			cmd:  lintCmd{Paths: []string{"/repo/apis"}, MaxWarnings: -1, wd: "/repo/apis"},
			want: want{err: "lint found 1 error", output: []string{"1 error, 0 warnings"}},
		},
		"ConfigFlag": {
			reason: "--config overrides the search.",
			files: map[string]string{
				"/repo/.crossplane-lint.yaml": lintConfigHeader + "rules:\n  xrd/no-bools: error\n",
				"/elsewhere/lint.yaml":        lintConfigHeader + "rules:\n  xrd/no-bools: off\n",
				"/repo/apis/xrd.yaml":         lintBoolXRD,
			},
			cmd:  lintCmd{Paths: []string{"/repo/apis"}, Config: "/elsewhere/lint.yaml", MaxWarnings: -1, wd: "/repo"},
			want: want{output: []string{"0 errors, 0 warnings"}},
		},
		"InvalidConfig": {
			reason: "An invalid config fails the run before any linting.",
			files: map[string]string{
				"/repo/.crossplane-lint.yaml": lintConfigHeader + "rules:\n  xrd/no-bools: [warn, {}]\n",
				"/repo/apis/xrd.yaml":         lintCleanXRD,
			},
			cmd:  lintCmd{Paths: []string{"/repo/apis"}, MaxWarnings: -1, wd: "/repo"},
			want: want{err: "list form is not supported"},
		},
		"UnknownRuleInConfig": {
			reason: "A config naming an unknown rule fails the run.",
			files: map[string]string{
				"/repo/.crossplane-lint.yaml": lintConfigHeader + "rules:\n  xrd/no-boools: warn\n",
				"/repo/apis/xrd.yaml":         lintCleanXRD,
			},
			cmd:  lintCmd{Paths: []string{"/repo/apis"}, MaxWarnings: -1, wd: "/repo"},
			want: want{err: `unknown rule "xrd/no-boools"`},
		},
		"MissingInput": {
			reason: "An input that cannot be read fails the run.",
			cmd:    lintCmd{Paths: []string{"/nope.yaml"}, MaxWarnings: -1},
			want:   want{err: "/nope.yaml"},
		},
		"NoXRDs": {
			reason: "Inputs holding no XRDs fail the run, so a wrong path does not pass silently.",
			files:  map[string]string{"/repo/apis/cm.yaml": "apiVersion: v1\nkind: ConfigMap\n"},
			cmd:    lintCmd{Paths: []string{"/repo/apis"}, MaxWarnings: -1},
			want:   want{err: "no XRDs found"},
		},
		"Stdin": {
			reason: "The path - reads from stdin.",
			cmd:    lintCmd{Paths: []string{"-"}, MaxWarnings: -1},
			stdin:  lintBoolXRD,
			want:   want{output: []string{"<stdin>", "0 errors, 1 warning"}},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			for p, d := range tc.files {
				if err := afero.WriteFile(fs, p, []byte(d), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			c := tc.cmd
			c.fs = fs
			c.stdin = strings.NewReader(tc.stdin)
			if c.wd == "" {
				c.wd = "/"
			}
			if c.Output == "" {
				c.Output = "text"
			}

			k := newKongContext(t)
			err := c.Run(k)
			out := k.Stdout.(*bytes.Buffer).String()

			if tc.want.err == "" && err != nil {
				t.Fatalf("\n%s\nRun(): unexpected error: %v\n%s", tc.reason, err, out)
			}
			if tc.want.err != "" && (err == nil || !strings.Contains(err.Error(), tc.want.err)) {
				t.Fatalf("\n%s\nRun(): want error containing %q, got %v", tc.reason, tc.want.err, err)
			}
			for _, s := range tc.want.output {
				if !strings.Contains(out, s) {
					t.Errorf("\n%s\nRun(): output does not contain %q:\n%s", tc.reason, s, out)
				}
			}
		})
	}
}

func TestLintRunJSON(t *testing.T) {
	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "/xrd.yaml", []byte(lintBoolXRD), 0o644); err != nil {
		t.Fatal(err)
	}
	c := lintCmd{Paths: []string{"/xrd.yaml"}, Output: "json", MaxWarnings: -1, fs: fs, wd: "/"}
	k := newKongContext(t)
	if err := c.Run(k); err != nil {
		t.Fatal(err)
	}

	got := &xrdlint.Result{}
	if err := json.Unmarshal(k.Stdout.(*bytes.Buffer).Bytes(), got); err != nil {
		t.Fatalf("cannot decode JSON output: %v", err)
	}
	if diff := cmp.Diff(xrdlint.Summary{Warnings: 1}, got.Summary); diff != "" {
		t.Errorf("summary: -want, +got:\n%s", diff)
	}
	if len(got.Findings) != 1 || got.Findings[0].Rule != "xrd/no-bools" || got.Findings[0].DocsURL == "" {
		t.Errorf("findings: want one xrd/no-bools finding with a docs URL, got %+v", got.Findings)
	}
}
