/*
Copyright 2025.

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

// Package storage provides shared utilities for building Kubernetes storage
// resources used across the resource-handler controllers.
//
// Currently this package contains [BuildPVCTemplate], which constructs
// PersistentVolumeClaim templates suitable for StatefulSet volumeClaimTemplates.
// It is used by the TopoServer controller to provision persistent storage for
// etcd data.
//
// The package exists as a separate shared utility so that storage construction
// logic is consistent across controllers and avoids duplication. Controllers
// that need PVC templates import this package rather than reimplementing the
// boilerplate for access modes, storage class, and resource request setup.
//
// # Usage
//
//	pvc := storage.BuildPVCTemplate("data", &storageClass, "10Gi", nil)
package storage
