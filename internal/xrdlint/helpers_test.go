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

	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
)

// xrdHeader is the start of a minimal v2 XRD. Line 15 holds the
// openAPIV3Schema key, and line 16 the first line of the schema that
// withSchema appends.
const xrdHeader = `apiVersion: apiextensions.crossplane.io/v2
kind: CompositeResourceDefinition
metadata:
  name: xnetworks.example.org
spec:
  group: example.org
  names:
    kind: XNetwork
    plural: xnetworks
  versions:
  - name: v1alpha1
    served: true
    referenceable: true
    schema:
      openAPIV3Schema:
`

// withSchema returns a minimal XRD whose single version has the supplied
// openAPIV3Schema. The schema must be indented by eight spaces.
func withSchema(schema string) string {
	return xrdHeader + schema
}

// parseOne parses data and fails the test unless it holds exactly one XRD.
func parseOne(t *testing.T, data string) *XRD {
	t.Helper()

	xrds, err := Parse("xrd.yaml", []byte(data))
	if err != nil {
		t.Fatalf("Parse(): %v", err)
	}
	if len(xrds) != 1 {
		t.Fatalf("Parse(): want 1 XRD, got %d", len(xrds))
	}
	return xrds[0]
}

// fakeRule is a SchemaRule whose behaviour is set by the test.
type fakeRule struct {
	meta  Meta
	check func(ctx *Context, xrd *XRD) error
}

func (r fakeRule) Meta() Meta { return r.meta }

func (r fakeRule) Check(ctx *Context, xrd *XRD) error {
	if r.check == nil {
		return nil
	}
	return r.check(ctx, xrd)
}

// reportWhere returns a check that reports every property for which match
// returns true.
func reportWhere(match func(p Property) bool, message string) func(ctx *Context, xrd *XRD) error {
	return func(ctx *Context, xrd *XRD) error {
		return xrd.Walk(func(p Property) error {
			if match(p) {
				ctx.Report(p, message)
			}
			return nil
		})
	}
}

func failing(msg string) func(*Context, *XRD) error {
	return func(*Context, *XRD) error { return errors.New(msg) }
}

func maxOptionsSchema() *extv1.JSONSchemaProps {
	return &extv1.JSONSchemaProps{
		Type: "object",
		Properties: map[string]extv1.JSONSchemaProps{
			"max": {Type: "integer", Minimum: new(1.0)},
		},
	}
}
