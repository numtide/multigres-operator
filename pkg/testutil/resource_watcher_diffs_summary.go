//go:build !verbose

package testutil

// showDiffs controls whether detailed diffs are logged when resources don't match.
// By default, only summary messages are shown.
// To enable detailed diffs, build with the "verbose" tag: go test -tags=verbose
const showDiffs = false
