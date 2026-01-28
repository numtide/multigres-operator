/*
Copyright 2026 Numtide.

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

package name

import (
	"strings"
	"testing"
)

// TestJoin checks determinism and uniqueness.
func TestJoin(t *testing.T) {
	// Check that it starts with the parts joined by '-'.
	if got, want := JoinWithConstraints(DefaultConstraints, "one", "two", "three"), "one-two-three-"; !strings.HasPrefix(
		got,
		want,
	) {
		t.Errorf("got %q, want prefix %q", got, want)
	}

	// Check determinism and uniqueness.
	table := []struct {
		name        string
		a, b        []string
		shouldEqual bool
	}{
		{
			name:        "same parts, same order",
			a:           []string{"one", "two", "three"},
			b:           []string{"one", "two", "three"},
			shouldEqual: true,
		},
		{
			name:        "same parts, different order",
			a:           []string{"one", "two", "three"},
			b:           []string{"one", "three", "two"},
			shouldEqual: false,
		},
		{
			name:        "different parts",
			a:           []string{"one", "two", "three"},
			b:           []string{"one", "two", "four"},
			shouldEqual: false,
		},
		{
			name:        "substring moved to adjacent part",
			a:           []string{"one-two", "three-four"},
			b:           []string{"one", "two-three-four"},
			shouldEqual: false,
		},
		{
			name:        "one part split into two parts",
			a:           []string{"one-two", "three-four"},
			b:           []string{"one-two", "three", "four"},
			shouldEqual: false,
		},
	}
	for _, test := range table {
		if got := JoinWithConstraints(DefaultConstraints, test.a...) == JoinWithConstraints(DefaultConstraints, test.b...); got != test.shouldEqual {
			t.Errorf("JoinWithConstraints: %s: got %v; want %v", test.name, got, test.shouldEqual)
		}
	}
}

// TestJoinHash checks that the hash function produces expected values.
func TestJoinHash(t *testing.T) {
	parts := []string{"hello", "world"}
	want := "hello-world-344ce285"
	if got := JoinWithConstraints(DefaultConstraints, parts...); got != want {
		t.Fatalf("JoinWithConstraints(%v) = %q, want %q", parts, got, want)
	}
}

func TestJoinWithConstraints(t *testing.T) {
	cons := Constraints{
		MaxLength:      50,
		ValidFirstChar: isLowercaseLetter,
	}

	table := []struct {
		input      []string
		wantPrefix string
	}{
		{
			input:      []string{"UpperCase-Letters", "Are-Lowercased"},
			wantPrefix: "uppercase-letters-are-lowercased-",
		},
		{
			input:      []string{"allowed-symbols", "dont---change"},
			wantPrefix: "allowed-symbols-dont---change-",
		},
		{
			input:      []string{"disallowed_symbols", "are.replaced"},
			wantPrefix: "disallowed-symbols-are-replaced-",
		},
		{
			input:      []string{"really-really-ridiculously-long-inputs-are-truncated"},
			wantPrefix: "really-really-ridiculously-long-inputs----",
		},
		{
			input:      []string{"-disallowed first chars", "-are prefixed"},
			wantPrefix: "x-disallowed-first-chars--are-prefixed-",
		},
		{
			input:      []string{"Transformed first char", "is ok"},
			wantPrefix: "transformed-first-char-is-ok-",
		},
	}

	for _, test := range table {
		got := JoinWithConstraints(cons, test.input...)
		if !strings.HasPrefix(got, test.wantPrefix) {
			t.Errorf(
				"JoinWithConstraints(%v) = %q; want prefix %q",
				test.input,
				got,
				test.wantPrefix,
			)
		}
	}
}

// TestJoinWithConstraintsMaxLength checks that values are truncated to fit
// within the max length.
func TestJoinWithConstraintsMaxLength(t *testing.T) {
	cons := Constraints{
		MaxLength:      25,
		ValidFirstChar: isLowercaseAlphanumeric,
	}

	// The total length after truncation should be equal to MaxLength.
	out := JoinWithConstraints(cons, strings.Repeat("a", 20), strings.Repeat("b", 20))
	if len(out) != cons.MaxLength {
		t.Errorf("len(%q) = %v; want %v", out, len(out), cons.MaxLength)
	}

	// The outputs should still be unique thanks to the hash suffix,
	// even if the truncated portion is the same because the difference between
	// inputs is at the end that gets cut off.
	out1 := JoinWithConstraints(cons, strings.Repeat("a", 20), strings.Repeat("b", 100)+"1")
	out2 := JoinWithConstraints(cons, strings.Repeat("a", 20), strings.Repeat("b", 100)+"2")
	if out1 == out2 {
		t.Errorf("got same output for two different inputs: %v", out1)
	}
}

// TestJoinWithConstraintsTransform checks that outputs are still
// distinguishable (thanks to the hash) even if inputs differ only in ways that
// are otherwise invisible after transformation.
func TestJoinWithConstraintsTransform(t *testing.T) {
	cons := Constraints{
		MaxLength:      50,
		ValidFirstChar: isLowercaseAlphanumeric,
	}

	// The outputs should still be unique thanks to the hash suffix,
	// even if the differences between inputs are otherwise invisible after
	// transformation.
	out1 := JoinWithConstraints(cons, "disallowed_symbol")
	out2 := JoinWithConstraints(cons, "disallowed/symbol")
	if out1 == out2 {
		t.Errorf("got same output for two different inputs: %v", out1)
	}
}

// TestCollisionPrevention verifies that the naming scheme prevents the collision
// scenario described in the issue.
func TestCollisionPrevention(t *testing.T) {
	// Scenario 1: MultigresCluster: production-db, Database: app, TableGroup: sales
	name1 := JoinWithConstraints(DefaultConstraints, "production-db", "app", "sales")

	// Scenario 2: MultigresCluster: production, Database: db-app, TableGroup: sales
	name2 := JoinWithConstraints(DefaultConstraints, "production", "db-app", "sales")

	// These should produce different names despite appearing identical before hashing
	if name1 == name2 {
		t.Errorf("collision detected: both scenarios produced the same name %q", name1)
	}

	// Verify both start with similar visible parts but have different hashes
	t.Logf("Scenario 1 name: %s", name1)
	t.Logf("Scenario 2 name: %s", name2)
}

// TestMultigresResourceNaming tests realistic naming patterns for multigres resources.
func TestMultigresResourceNaming(t *testing.T) {
	tests := []struct {
		name     string
		parts    []string
		wantLen  int
		wantHash bool
	}{
		{
			name:     "cell name",
			parts:    []string{"production", "zone-a"},
			wantLen:  -1, // don't check
			wantHash: true,
		},
		{
			name:     "tablegroup name",
			parts:    []string{"production", "mydb", "default"},
			wantLen:  -1,
			wantHash: true,
		},
		{
			name:     "shard name",
			parts:    []string{"production-mydb-default-5c6a71f9", "0"},
			wantLen:  -1,
			wantHash: true,
		},
		{
			name:     "global topo name",
			parts:    []string{"production", "global-topo"},
			wantLen:  -1,
			wantHash: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := JoinWithConstraints(DefaultConstraints, tt.parts...)

			if tt.wantLen > 0 && len(got) != tt.wantLen {
				t.Errorf("len(%s) = %d, want %d", got, len(got), tt.wantLen)
			}

			if tt.wantHash {
				// Check that it has a hash at the end (8 hex chars after last hyphen)
				parts := strings.Split(got, "-")
				lastPart := parts[len(parts)-1]
				if len(lastPart) != hashLength {
					t.Errorf("expected hash suffix of length %d, got %q", hashLength, lastPart)
				}
			}

			t.Logf("Generated name: %s", got)
		})
	}
}

// TestServiceConstraints verifies that service constraints work correctly.
func TestServiceConstraints(t *testing.T) {
	parts := []string{"production", "gateway"}
	got := JoinWithConstraints(ServiceConstraints, parts...)

	if len(got) > 63 {
		t.Errorf("service name %q exceeds 63 character limit (got %d)", got, len(got))
	}

	// Should start with a lowercase letter
	if !isLowercaseLetter(rune(got[0])) {
		t.Errorf("service name %q does not start with lowercase letter", got)
	}

	t.Logf("Service name: %s (length: %d)", got, len(got))
}

// TestInvalidConstraintsPanic verifies that invalid constraints cause a panic.
func TestInvalidConstraintsPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic for invalid constraints")
		}
	}()

	invalidCons := Constraints{
		MaxLength:      5, // Too short
		ValidFirstChar: isLowercaseLetter,
	}

	JoinWithConstraints(invalidCons, "test")
}

// TestEmptyParts verifies handling of empty input.
func TestEmptyParts(t *testing.T) {
	got := JoinWithConstraints(DefaultConstraints)
	if got != "" {
		t.Errorf("expected empty string for empty parts, got %q", got)
	}
}
