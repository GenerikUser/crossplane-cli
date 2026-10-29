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

package rules

import (
	"testing"

	"github.com/crossplane/cli/v2/internal/xrdlint"
	"github.com/crossplane/cli/v2/internal/xrdlint/xrdlinttest"
)

func TestBuiltin(t *testing.T) {
	reg, err := xrdlint.NewRegistry(Builtin()...)
	if err != nil {
		t.Fatalf("NewRegistry(Builtin()...): %v", err)
	}

	want := map[string]xrdlint.Severity{
		"xrd/status-fields-optional":     xrdlint.SeverityError,
		"xrd/status-no-value-validation": xrdlint.SeverityError,
		"xrd/lists-need-list-type":       xrdlint.SeverityError,
		"xrd/no-bools":                   xrdlint.SeverityWarn,
	}
	for _, r := range reg.All() {
		m := r.Meta()
		sev, ok := want[m.ID]
		if !ok {
			t.Errorf("unexpected built-in rule %q", m.ID)
			continue
		}
		if m.DefaultSeverity != sev {
			t.Errorf("%s: want default severity %q, got %q", m.ID, sev, m.DefaultSeverity)
		}
		if m.DocsURL == "" || m.Description == "" {
			t.Errorf("%s: built-in rules need a description and a docs URL", m.ID)
		}
		delete(want, m.ID)
	}
	for id := range want {
		t.Errorf("missing built-in rule %q", id)
	}
}

func TestStatusFieldsOptional(t *testing.T) {
	xrdlinttest.Run(t, StatusFieldsOptional{}, map[string]xrdlinttest.Case{
		"NoRequired": {
			Reason: "A status with no required list passes.",
			XRD: xrdlinttest.XRD(`
type: object
properties:
  status:
    type: object
    properties:
      ready:
        type: string
`),
		},
		"RequiredInSpec": {
			Reason: "Required fields under spec are fine.",
			XRD: xrdlinttest.XRD(`
type: object
properties:
  spec:
    type: object
    required: [region]
    properties:
      region:
        type: string
`),
		},
		"RequiredOnStatus": {
			Reason: "A required list on status itself is flagged.",
			XRD: xrdlinttest.XRD(`
type: object
properties:
  status:
    type: object
    required: [ready]
    properties:
      ready:
        type: string
`),
			Want: []xrdlinttest.Want{{Path: "status", Message: "status fields must be optional; remove required: [ready]"}},
		},
		"RequiredNested": {
			Reason: "A required list anywhere beneath status is flagged, including in array items.",
			XRD: xrdlinttest.XRD(`
type: object
properties:
  status:
    type: object
    properties:
      endpoints:
        type: array
        x-kubernetes-list-type: atomic
        items:
          type: object
          required: [host, port]
          properties:
            host:
              type: string
            port:
              type: integer
`),
			Want: []xrdlinttest.Want{{Path: "status.endpoints[*]"}},
		},
		"StatusRequiredAtRoot": {
			Reason: "Requiring status itself at the schema root is flagged.",
			XRD: xrdlinttest.XRD(`
type: object
required: [status]
properties:
  status:
    type: object
`),
			Want: []xrdlinttest.Want{{Path: "status", Message: "status must not be required"}},
		},
	})
}

func TestStatusNoValueValidation(t *testing.T) {
	xrdlinttest.Run(t, StatusNoValueValidation{}, map[string]xrdlinttest.Case{
		"TypesOnly": {
			Reason: "Types and descriptions under status are fine.",
			XRD: xrdlinttest.XRD(`
type: object
properties:
  status:
    type: object
    properties:
      cidr:
        type: string
        description: The observed CIDR.
`),
		},
		"ValidationInSpec": {
			Reason: "Value validation under spec is fine.",
			XRD: xrdlinttest.XRD(`
type: object
properties:
  spec:
    type: object
    properties:
      cidr:
        type: string
        pattern: '^[0-9./]+$'
`),
		},
		"Pattern": {
			Reason: "A pattern under status is flagged.",
			XRD: xrdlinttest.XRD(`
type: object
properties:
  status:
    type: object
    properties:
      cidr:
        type: string
        pattern: '^[0-9./]+$'
`),
			Want: []xrdlinttest.Want{{Path: "status.cidr", Message: "status field must not set pattern"}},
		},
		"Several": {
			Reason: "Several keywords on one field are reported together, in a stable order.",
			XRD: xrdlinttest.XRD(`
type: object
properties:
  status:
    type: object
    properties:
      count:
        type: integer
        minimum: 0
        maximum: 10
      phase:
        type: string
        enum: [Ready, NotReady]
        maxLength: 8
      items:
        type: array
        x-kubernetes-list-type: atomic
        maxItems: 3
        items:
          type: string
`),
			Want: []xrdlinttest.Want{
				{Path: "status.count", Message: "status field must not set minimum, maximum"},
				{Path: "status.phase", Message: "status field must not set maxLength, enum"},
				{Path: "status.items", Message: "status field must not set maxItems"},
			},
		},
	})
}

func TestListsNeedListType(t *testing.T) {
	xrdlinttest.Run(t, ListsNeedListType{}, map[string]xrdlinttest.Case{
		"Atomic": {
			Reason: "An array with an atomic list type passes.",
			XRD: xrdlinttest.XRD(`
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
`),
		},
		"Map": {
			Reason: "An array with a map list type passes.",
			XRD: xrdlinttest.XRD(`
type: object
properties:
  spec:
    type: object
    properties:
      subnets:
        type: array
        x-kubernetes-list-type: map
        x-kubernetes-list-map-keys: [name]
        items:
          type: object
          required: [name]
          properties:
            name:
              type: string
`),
		},
		"Missing": {
			Reason: "An array with no list type is flagged, wherever it is.",
			XRD: xrdlinttest.XRD(`
type: object
properties:
  spec:
    type: object
    properties:
      zones:
        type: array
        items:
          type: string
  status:
    type: object
    properties:
      addresses:
        type: array
        items:
          type: string
`),
			Want: []xrdlinttest.Want{
				{Path: "spec.zones", Message: "array has no x-kubernetes-list-type"},
				{Path: "status.addresses"},
			},
		},
	})
}

func TestNoBools(t *testing.T) {
	xrdlinttest.Run(t, NoBools{}, map[string]xrdlinttest.Case{
		"Enum": {
			Reason: "A string enum passes.",
			XRD: xrdlinttest.XRD(`
type: object
properties:
  spec:
    type: object
    properties:
      visibility:
        type: string
        enum: [Public, Private]
`),
		},
		"Bool": {
			Reason: "A boolean is flagged, in spec and in status.",
			XRD: xrdlinttest.XRD(`
type: object
properties:
  spec:
    type: object
    properties:
      public:
        type: boolean
  status:
    type: object
    properties:
      ready:
        type: boolean
`),
			Want: []xrdlinttest.Want{
				{Path: "spec.public", Message: "prefer an enum to a boolean"},
				{Path: "status.ready"},
			},
		},
	})
}
