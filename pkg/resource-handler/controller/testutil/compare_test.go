package testutil_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/numtide/multigres-operator/pkg/resource-handler/controller/testutil"
)

func TestComparisonOptions(t *testing.T) {
	tests := map[string]struct {
		obj1    any
		obj2    any
		options cmp.Options
	}{
		"IgnoreMetaRuntimeFields": {
			obj1: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc",
					Namespace: "default",
					// Runtime fields differ - should be ignored
					UID:               "uid1",
					ResourceVersion:   "123",
					CreationTimestamp: metav1.Time{Time: time.Now()},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
				},
			},
			obj2: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc",
					Namespace: "default",
					// Runtime fields differ - should be ignored
					UID:               "different-uid",
					ResourceVersion:   "999",
					CreationTimestamp: metav1.Time{Time: time.Now().Add(1 * time.Hour)},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP, // Must match
				},
			},
			options: testutil.IgnoreMetaRuntimeFields(),
		},
		"IgnoreStatus": {
			obj1: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
				},
				Status: corev1.ServiceStatus{
					// Status differs - should be ignored
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{{IP: "1.2.3.4"}},
					},
				},
			},
			obj2: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP, // Must match
				},
				Status: corev1.ServiceStatus{
					// Status differs - should be ignored
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{{IP: "5.6.7.8"}},
					},
				},
			},
			options: cmp.Options{testutil.IgnoreStatus()},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			diff := cmp.Diff(tc.obj1, tc.obj2, tc.options...)
			if diff != "" {
				t.Errorf("%s should make objects match, but found diff:\n%s", name, diff)
			}
		})
	}
}
