package shard

// // buildLabels creates standard labels for Shard related resources.
// // Uses the pool's cell names from the Cell field.
// func buildLabels(
// 	shard *multigresv1alpha1.Shard,
// 	poolName string,
// ) map[string]string {
// 	cs := []string{}
// 	for _, p := range shard.Spec.Pools {
// 		cs = append(cs, p.Cell)
// 	}
// 	cellName := strings.Join(cs, ",")
// 	if cellName == "" {
// 		cellName = metadata.DefaultCellName
// 	}

// 	labels := metadata.BuildStandardLabels(poolName, PoolComponentName)
// 	// Merge any labels associated from Shard.
// 	maps.Copy(labels, shard.GetObjectMeta().GetLabels())

// 	return labels
// }
