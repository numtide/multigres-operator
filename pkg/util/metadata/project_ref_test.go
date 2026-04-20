package metadata_test

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/multigres/multigres-operator/pkg/util/metadata"
)

func TestResolveProjectRef_Property(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		clusterName := rapid.String().Draw(t, "cluster_name")
		projectRef := rapid.String().Draw(t, "project_ref")
		hasAnnotation := rapid.Bool().Draw(t, "has_annotation")
		useNilMap := rapid.Bool().Draw(t, "use_nil_map")

		var annotations map[string]string
		if !useNilMap {
			annotations = map[string]string{}
		}
		if hasAnnotation {
			if annotations == nil {
				annotations = map[string]string{}
			}
			annotations[metadata.AnnotationProjectRef] = projectRef
		}

		want := clusterName
		if hasAnnotation && projectRef != "" {
			want = projectRef
		}

		got := metadata.ResolveProjectRef(annotations, clusterName)
		if got != want {
			t.Fatalf("ResolveProjectRef() = %q, want %q", got, want)
		}
	})
}
