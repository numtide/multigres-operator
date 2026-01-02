// Package tablegroup implements the controller for the TableGroup resource.
//
// The TableGroup controller acts as a "middle-manager" in the Multigres hierarchy,
// sitting between the root MultigresCluster and the leaf Shard resources.
//
// Responsibilities:
//
//  1. Shard Lifecycle Management:
//     It watches TableGroup resources and creates, updates, or deletes (prunes)
//     child Shard resources to match the list of fully resolved shard specifications
//     provided in the TableGroup spec.
//
//  2. Status Aggregation:
//     It monitors the status of all owned Shards and aggregates them into the
//     TableGroup's status (e.g., ReadyShards / TotalShards), providing a summarized
//     view for the parent MultigresCluster controller.
//
// Design Note:
// This controller is intentionally "dumb" regarding configuration logic. It does not
// perform template resolution or defaulting. It expects fully resolved specs to be
// pushed down from the parent MultigresCluster controller, and its only job is to
// enforce that state on the child Shards.
package tablegroup
