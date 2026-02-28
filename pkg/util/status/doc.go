// Package status provides shared helpers for managing the lifecycle state of
// Multigres Custom Resources.
//
// It contains utilities for:
//   - Computing resource Phase (Initializing / Progressing / Healthy) from
//     replica counts via ComputePhase.
//   - Managing metav1.Condition slices (SetCondition, IsConditionTrue) used
//     by data-handler controllers to track topology registration, backup
//     health, and similar observable state.
//
// These helpers are intentionally kept in a shared package so that both the
// Cell and Shard data-handler controllers (and any future controllers) use
// a single implementation rather than duplicating condition logic.
package status
