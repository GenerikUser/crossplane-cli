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
	"bytes"
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"sigs.k8s.io/yaml"
)

func outputFixture() *Result {
	return &Result{
		Summary: Summary{Errors: 2, Warnings: 1},
		Findings: []Finding{
			{Rule: "xrd/lists-need-list-type", Severity: SeverityError, File: "apis/network/definition.yaml", Line: 41, Column: 9, XRD: "xnetworks.example.org", Version: "v1alpha1", Path: "spec.subnets", Message: "array has no x-kubernetes-list-type"},
			{Rule: "xrd/no-bools", Severity: SeverityWarn, File: "apis/network/definition.yaml", Line: 52, Column: 11, XRD: "xnetworks.example.org", Version: "v1alpha1", Path: "spec.public", Message: "prefer an enum to a boolean"},
			{Rule: "lint/invalid-xrd", Severity: SeverityError, File: "apis/bucket/definition.yaml", Line: 1, Column: 1, XRD: "xbuckets.example.org", Message: "schema is not structural"},
		},
	}
}

func TestRenderText(t *testing.T) {
	r, err := RendererFor(FormatText)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := r.Render(outputFixture(), &buf); err != nil {
		t.Fatal(err)
	}

	want := `apis/network/definition.yaml
  xnetworks.example.org v1alpha1
    41:9   error  spec.subnets: array has no x-kubernetes-list-type  xrd/lists-need-list-type
    52:11  warn   spec.public: prefer an enum to a boolean           xrd/no-bools

apis/bucket/definition.yaml
  xbuckets.example.org
    1:1  error  schema is not structural  lint/invalid-xrd

2 errors, 1 warning
`
	if diff := cmp.Diff(want, buf.String()); diff != "" {
		t.Errorf("Render(text): -want, +got:\n%s", diff)
	}
}

func TestRenderTextClean(t *testing.T) {
	r, _ := RendererFor("")
	var buf bytes.Buffer
	if err := r.Render(&Result{}, &buf); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff("0 errors, 0 warnings\n", buf.String()); diff != "" {
		t.Errorf("Render(text): -want, +got:\n%s", diff)
	}
}

func TestRenderStructured(t *testing.T) {
	cases := map[string]struct {
		format    Format
		unmarshal func([]byte, any) error
	}{
		"JSON": {format: FormatJSON, unmarshal: json.Unmarshal},
		"YAML": {format: FormatYAML, unmarshal: func(b []byte, v any) error { return yaml.Unmarshal(b, v) }},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r, err := RendererFor(tc.format)
			if err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			if err := r.Render(outputFixture(), &buf); err != nil {
				t.Fatal(err)
			}
			got := &Result{}
			if err := tc.unmarshal(buf.Bytes(), got); err != nil {
				t.Fatalf("cannot decode %s output: %v\n%s", name, err, buf.String())
			}
			if diff := cmp.Diff(outputFixture(), got); diff != "" {
				t.Errorf("Render(%s): round trip -want, +got:\n%s", name, diff)
			}
		})
	}
}

func TestRenderStructuredEmptyFindings(t *testing.T) {
	r, _ := RendererFor(FormatJSON)
	var buf bytes.Buffer
	if err := r.Render(&Result{}, &buf); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"summary\": {\n    \"errors\": 0,\n    \"warnings\": 0\n  },\n  \"findings\": []\n}\n"
	if diff := cmp.Diff(want, buf.String()); diff != "" {
		t.Errorf("Render(json): -want, +got:\n%s", diff)
	}
}

func TestRendererForUnknown(t *testing.T) {
	if _, err := RendererFor("xml"); err == nil {
		t.Error("RendererFor(xml): want error, got nil")
	}
}
