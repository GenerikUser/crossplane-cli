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

const simpleSchema = `        type: object
        properties:
          spec:
            type: object
`

func TestParse(t *testing.T) {
	type want struct {
		names []string
		err   string
	}

	cases := map[string]struct {
		reason string
		data   string
		want   want
	}{
		"V2": {
			reason: "A v2 XRD is loaded.",
			data:   withSchema(simpleSchema),
			want:   want{names: []string{"xnetworks.example.org"}},
		},
		"V1": {
			reason: "A v1 XRD is loaded too.",
			data:   strings.Replace(withSchema(simpleSchema), "crossplane.io/v2", "crossplane.io/v1", 1),
			want:   want{names: []string{"xnetworks.example.org"}},
		},
		"SkipsOtherKinds": {
			reason: "Documents that are not XRDs are skipped.",
			data: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n---\n" +
				withSchema(simpleSchema) + "---\n",
			want: want{names: []string{"xnetworks.example.org"}},
		},
		"SkipsOtherGroups": {
			reason: "A CompositeResourceDefinition kind in another group is skipped.",
			data:   "apiVersion: example.org/v1\nkind: CompositeResourceDefinition\n",
			want:   want{names: nil},
		},
		"InvalidYAML": {
			reason: "Unparseable YAML is an error that names the file.",
			data:   "apiVersion: [",
			want:   want{err: "xrd.yaml"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := Parse("xrd.yaml", []byte(tc.data))
			if tc.want.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want.err) {
					t.Fatalf("\n%s\nParse(): want error containing %q, got %v", tc.reason, tc.want.err, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("\n%s\nParse(): unexpected error: %v", tc.reason, err)
			}
			var names []string
			for _, x := range got {
				names = append(names, x.Name)
			}
			if diff := cmp.Diff(tc.want.names, names); diff != "" {
				t.Errorf("\n%s\nParse(): -want names, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	fs := afero.NewMemMapFs()
	files := map[string]string{
		"/apis/a/definition.yaml":  withSchema(simpleSchema),
		"/apis/b/definition.yml":   strings.Replace(withSchema(simpleSchema), "xnetworks.example.org", "xbuckets.example.org", 1),
		"/apis/b/README.md":        "not yaml",
		"/apis/b/composition.yaml": "apiVersion: apiextensions.crossplane.io/v1\nkind: Composition\n",
		"/single.yaml":             strings.Replace(withSchema(simpleSchema), "xnetworks.example.org", "xsingles.example.org", 1),
	}
	for p, d := range files {
		if err := afero.WriteFile(fs, p, []byte(d), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stdin := strings.NewReader(strings.Replace(withSchema(simpleSchema), "xnetworks.example.org", "xstdins.example.org", 1))

	got, err := Load(fs, stdin, []string{"/apis", "/single.yaml", "-"})
	if err != nil {
		t.Fatalf("Load(): unexpected error: %v", err)
	}

	type loaded struct{ File, Name string }
	gotLoaded := make([]loaded, 0, len(got))
	for _, x := range got {
		gotLoaded = append(gotLoaded, loaded{File: x.File, Name: x.Name})
	}
	want := []loaded{
		{File: "/apis/a/definition.yaml", Name: "xnetworks.example.org"},
		{File: "/apis/b/definition.yml", Name: "xbuckets.example.org"},
		{File: "/single.yaml", Name: "xsingles.example.org"},
		{File: "<stdin>", Name: "xstdins.example.org"},
	}
	if diff := cmp.Diff(want, gotLoaded); diff != "" {
		t.Errorf("Load(): -want, +got:\n%s", diff)
	}

	if _, err := Load(fs, nil, []string{"/missing.yaml"}); err == nil {
		t.Error("Load(): want error for a missing input, got nil")
	}
}

func TestWalk(t *testing.T) {
	data := xrdHeader + `        type: object
        required: [spec]
        properties:
          spec:
            type: object
            required: [region]
            properties:
              region:
                type: string
              subnets:
                type: array
                items:
                  type: object
                  properties:
                    cidr:
                      type: string
              tags:
                type: object
                additionalProperties:
                  type: string
          status:
            type: object
            properties:
              ready:
                type: boolean
  - name: v1beta1
    served: false
    referenceable: false
    schema:
      openAPIV3Schema:
        type: object
`
	x := parseOne(t, data)

	type visited struct {
		Version  string
		Path     string
		Line     int
		InStatus bool
		Required bool
	}
	var got []visited
	if err := x.Walk(func(p Property) error {
		got = append(got, visited{Version: p.Version, Path: p.Path, Line: p.Line(), InStatus: p.InStatus, Required: p.Required})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	want := []visited{
		{Version: "v1alpha1", Path: "", Line: 15},
		{Version: "v1alpha1", Path: "spec", Line: 19, Required: true},
		{Version: "v1alpha1", Path: "spec.region", Line: 23, Required: true},
		{Version: "v1alpha1", Path: "spec.subnets", Line: 25},
		{Version: "v1alpha1", Path: "spec.subnets[*]", Line: 27},
		{Version: "v1alpha1", Path: "spec.subnets[*].cidr", Line: 30},
		{Version: "v1alpha1", Path: "spec.tags", Line: 32},
		{Version: "v1alpha1", Path: "spec.tags.*", Line: 34},
		{Version: "v1alpha1", Path: "status", Line: 36, InStatus: true},
		{Version: "v1alpha1", Path: "status.ready", Line: 39, InStatus: true},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Walk(): -want, +got:\n%s", diff)
	}
}
