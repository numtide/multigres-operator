// Package testutil provides test utilities for Kubernetes operators. The main
// support is around envtest and resource watching logic, where you can use
// the following code to wait for the matching resource to appear.
//
// Example:
//
//	watcher.SetCmpOpts(testutil.CompareSpecOnly()...)
//	expectedSts := &appsv1.StatefulSet{
//	    Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(3))},
//	}
//	expectedSvc := &corev1.Service{...}
//
//	err := watcher.WaitForMatch(expectedSts, expectedSvc)
//	if err != nil {
//	    t.Errorf("Resources never reached expected state: %v", err)
//	}
//
// This utility allows resource matching logic to be fully declarative, and
// while integration tests are more expensive to run in terms of time spent,
// this makes sure that there is no unnecessary sleep logic, and all the tests
// can be run in an event driven fashion.
package testutil
