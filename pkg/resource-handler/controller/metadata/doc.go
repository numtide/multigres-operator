// Package metadata provides utilities for building Kubernetes resource metadata
// such as labels, annotations, and owner references used across all Multigres components.
//
// This package contains generic, reusable functions that are component-agnostic.
// Component-specific logic should be provided by callers through function parameters.
package metadata
