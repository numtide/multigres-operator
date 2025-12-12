//go:build verbose

package envtestutil

// showDiffs controls whether detailed diffs are logged when resources don't match.
// This file is compiled when building with the "verbose" tag: go test -tags=verbose
const showDiffs = true
