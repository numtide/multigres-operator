//go:build e2e

// Package e2e_test contains end-to-end tests that run the full multigres
// operator against a kind cluster. Each test function gets its own isolated
// namespace via SetUpKindManager.
//
// These tests verify the complete resource provisioning chain:
//
//	MultigresCluster → TopoServer, Cell, TableGroup, Deployment(multiadmin)
//	  Cell → Deployment(multigateway), Service
//	  TopoServer → StatefulSet(etcd), Service, Service(headless)
//	  TableGroup → Shard
//	    Shard → StatefulSet(postgres), Deployment(multiorch), Service, ConfigMap, Secret
package e2e_test
