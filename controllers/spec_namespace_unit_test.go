/*
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

package controllers

import (
	"regexp"
	"testing"
)

func TestSpecNamespacePrefixDisabled(t *testing.T) {
	t.Run("enabled by default", func(t *testing.T) {
		t.Setenv("DEPLOY_OPERATOR", "")
		t.Setenv("TEST_DISABLE_NS_PREFIX", "")
		if namespaceIsolationDisabled() {
			t.Fatal("prefixing should be enabled by default")
		}
	})

	t.Run("disabled by TEST_DISABLE_NS_PREFIX", func(t *testing.T) {
		t.Setenv("DEPLOY_OPERATOR", "")
		t.Setenv("TEST_DISABLE_NS_PREFIX", "true")
		if !namespaceIsolationDisabled() {
			t.Fatal("TEST_DISABLE_NS_PREFIX=true should disable prefixing")
		}
	})

	t.Run("TEST_DISABLE_NS_PREFIX false keeps prefixing", func(t *testing.T) {
		t.Setenv("DEPLOY_OPERATOR", "")
		t.Setenv("TEST_DISABLE_NS_PREFIX", "false")
		if namespaceIsolationDisabled() {
			t.Fatal("TEST_DISABLE_NS_PREFIX=false should keep prefixing enabled")
		}
	})

	t.Run("disabled by DEPLOY_OPERATOR", func(t *testing.T) {
		t.Setenv("DEPLOY_OPERATOR", "true")
		t.Setenv("TEST_DISABLE_NS_PREFIX", "false")
		if !namespaceIsolationDisabled() {
			t.Fatal("DEPLOY_OPERATOR=true should disable prefixing")
		}
	})
}

func TestUniqueSpecNamespace(t *testing.T) {
	prefixed := regexp.MustCompile(`^[a-z0-9]+-[0-9a-f]{6}$`)

	t.Run("adds a 6 hex suffix when prefixing", func(t *testing.T) {
		t.Setenv("DEPLOY_OPERATOR", "")
		t.Setenv("TEST_DISABLE_NS_PREFIX", "false")

		name := uniqueSpecNamespace("test")
		if !prefixed.MatchString(name) {
			t.Fatalf("got %q, want test-<6 hex>", name)
		}
		if name[:5] != "test-" {
			t.Fatalf("got %q, want prefix test-", name)
		}

		other := uniqueSpecNamespace("other")
		if other[:6] != "other-" || !prefixed.MatchString(other) {
			t.Fatalf("got %q, want other-<6 hex>", other)
		}

		first := uniqueSpecNamespace("test")
		second := uniqueSpecNamespace("test")
		if first == second {
			t.Fatalf("expected unique names, both were %q", first)
		}
	})

	t.Run("returns the original name when prefixing is disabled", func(t *testing.T) {
		t.Setenv("DEPLOY_OPERATOR", "")
		t.Setenv("TEST_DISABLE_NS_PREFIX", "true")

		if got := uniqueSpecNamespace("test"); got != "test" {
			t.Fatalf("got %q, want test", got)
		}
		if got := uniqueSpecNamespace("restricted"); got != "restricted" {
			t.Fatalf("got %q, want restricted", got)
		}
	})

	t.Run("returns the original name when DEPLOY_OPERATOR is true", func(t *testing.T) {
		t.Setenv("DEPLOY_OPERATOR", "true")
		t.Setenv("TEST_DISABLE_NS_PREFIX", "false")

		if got := uniqueSpecNamespace("test"); got != "test" {
			t.Fatalf("got %q, want test", got)
		}
	})
}

func TestSpecNamespaceBase(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{name: "test-a3f1b9", want: "test"},
		{name: "other-abcdef", want: "other"},
		{name: "ns-one-00ffaa", want: "ns-one"},
		{name: "test", want: "test"},
		{name: "other-ns", want: "other-ns"},
	}
	for _, tc := range cases {
		if got := specNamespaceBase(tc.name); got != tc.want {
			t.Errorf("specNamespaceBase(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}
