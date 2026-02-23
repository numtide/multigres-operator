//go:build e2e

// Package e2e_test contains end-to-end tests that run the full multigres
// operator against a kind cluster. Each test function gets its own isolated
// namespace via SetUpKindManager.
package e2e_test
