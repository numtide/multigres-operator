package etcd

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
)

func TestBuildPodIdentityEnv(t *testing.T) {
	got := buildPodIdentityEnv()

	want := []corev1.EnvVar{
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		},
		{
			Name: "POD_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		},
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("buildPodIdentityEnv() mismatch (-want +got):\n%s", diff)
	}
}

func TestBuildEtcdConfigEnv(t *testing.T) {
	tests := map[string]struct {
		etcdName    string
		serviceName string
		namespace   string
		want        []corev1.EnvVar
	}{
		"basic configuration": {
			etcdName:    "my-etcd",
			serviceName: "my-etcd-headless",
			namespace:   "default",
			want: []corev1.EnvVar{
				{Name: "ETCD_NAME", Value: "$(POD_NAME)"},
				{Name: "ETCD_DATA_DIR", Value: "/var/lib/etcd"},
				{Name: "ETCD_LISTEN_CLIENT_URLS", Value: "http://0.0.0.0:2379"},
				{Name: "ETCD_LISTEN_PEER_URLS", Value: "http://0.0.0.0:2380"},
				{Name: "ETCD_ADVERTISE_CLIENT_URLS", Value: "http://$(POD_NAME).my-etcd-headless.$(POD_NAMESPACE).svc.cluster.local:2379"},
				{Name: "ETCD_INITIAL_ADVERTISE_PEER_URLS", Value: "http://$(POD_NAME).my-etcd-headless.$(POD_NAMESPACE).svc.cluster.local:2380"},
				{Name: "ETCD_INITIAL_CLUSTER_STATE", Value: "new"},
				{Name: "ETCD_INITIAL_CLUSTER_TOKEN", Value: "my-etcd"},
			},
		},
		"different namespace": {
			etcdName:    "test-etcd",
			serviceName: "test-etcd-headless",
			namespace:   "production",
			want: []corev1.EnvVar{
				{Name: "ETCD_NAME", Value: "$(POD_NAME)"},
				{Name: "ETCD_DATA_DIR", Value: "/var/lib/etcd"},
				{Name: "ETCD_LISTEN_CLIENT_URLS", Value: "http://0.0.0.0:2379"},
				{Name: "ETCD_LISTEN_PEER_URLS", Value: "http://0.0.0.0:2380"},
				{Name: "ETCD_ADVERTISE_CLIENT_URLS", Value: "http://$(POD_NAME).test-etcd-headless.$(POD_NAMESPACE).svc.cluster.local:2379"},
				{Name: "ETCD_INITIAL_ADVERTISE_PEER_URLS", Value: "http://$(POD_NAME).test-etcd-headless.$(POD_NAMESPACE).svc.cluster.local:2380"},
				{Name: "ETCD_INITIAL_CLUSTER_STATE", Value: "new"},
				{Name: "ETCD_INITIAL_CLUSTER_TOKEN", Value: "test-etcd"},
			},
		},
		"long names": {
			etcdName:    "very-long-etcd-cluster-name",
			serviceName: "very-long-etcd-cluster-name-headless",
			namespace:   "kube-system",
			want: []corev1.EnvVar{
				{Name: "ETCD_NAME", Value: "$(POD_NAME)"},
				{Name: "ETCD_DATA_DIR", Value: "/var/lib/etcd"},
				{Name: "ETCD_LISTEN_CLIENT_URLS", Value: "http://0.0.0.0:2379"},
				{Name: "ETCD_LISTEN_PEER_URLS", Value: "http://0.0.0.0:2380"},
				{Name: "ETCD_ADVERTISE_CLIENT_URLS", Value: "http://$(POD_NAME).very-long-etcd-cluster-name-headless.$(POD_NAMESPACE).svc.cluster.local:2379"},
				{Name: "ETCD_INITIAL_ADVERTISE_PEER_URLS", Value: "http://$(POD_NAME).very-long-etcd-cluster-name-headless.$(POD_NAMESPACE).svc.cluster.local:2380"},
				{Name: "ETCD_INITIAL_CLUSTER_STATE", Value: "new"},
				{Name: "ETCD_INITIAL_CLUSTER_TOKEN", Value: "very-long-etcd-cluster-name"},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := buildEtcdConfigEnv(tc.etcdName, tc.serviceName, tc.namespace)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("buildEtcdConfigEnv() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildEtcdClusterPeerList(t *testing.T) {
	tests := map[string]struct {
		etcdName    string
		serviceName string
		namespace   string
		replicas    int32
		want        string
	}{
		"single replica": {
			etcdName:    "my-etcd",
			serviceName: "my-etcd-headless",
			namespace:   "default",
			replicas:    1,
			want:        "my-etcd-0=http://my-etcd-0.my-etcd-headless.default.svc.cluster.local:2380",
		},
		"three replicas (typical HA)": {
			etcdName:    "my-etcd",
			serviceName: "my-etcd-headless",
			namespace:   "default",
			replicas:    3,
			want:        "my-etcd-0=http://my-etcd-0.my-etcd-headless.default.svc.cluster.local:2380,my-etcd-1=http://my-etcd-1.my-etcd-headless.default.svc.cluster.local:2380,my-etcd-2=http://my-etcd-2.my-etcd-headless.default.svc.cluster.local:2380",
		},
		"five replicas": {
			etcdName:    "etcd-prod",
			serviceName: "etcd-prod-headless",
			namespace:   "production",
			replicas:    5,
			want:        "etcd-prod-0=http://etcd-prod-0.etcd-prod-headless.production.svc.cluster.local:2380,etcd-prod-1=http://etcd-prod-1.etcd-prod-headless.production.svc.cluster.local:2380,etcd-prod-2=http://etcd-prod-2.etcd-prod-headless.production.svc.cluster.local:2380,etcd-prod-3=http://etcd-prod-3.etcd-prod-headless.production.svc.cluster.local:2380,etcd-prod-4=http://etcd-prod-4.etcd-prod-headless.production.svc.cluster.local:2380",
		},
		"zero replicas": {
			etcdName:    "my-etcd",
			serviceName: "my-etcd-headless",
			namespace:   "default",
			replicas:    0,
			want:        "",
		},
		"negative replicas": {
			etcdName:    "my-etcd",
			serviceName: "my-etcd-headless",
			namespace:   "default",
			replicas:    -1,
			want:        "",
		},
		"different namespace": {
			etcdName:    "kube-etcd",
			serviceName: "kube-etcd-headless",
			namespace:   "kube-system",
			replicas:    3,
			want:        "kube-etcd-0=http://kube-etcd-0.kube-etcd-headless.kube-system.svc.cluster.local:2380,kube-etcd-1=http://kube-etcd-1.kube-etcd-headless.kube-system.svc.cluster.local:2380,kube-etcd-2=http://kube-etcd-2.kube-etcd-headless.kube-system.svc.cluster.local:2380",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := buildEtcdClusterPeerList(tc.etcdName, tc.serviceName, tc.namespace, tc.replicas)
			if got != tc.want {
				t.Errorf("buildEtcdClusterPeerList() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBuildEtcdEnv(t *testing.T) {
	tests := map[string]struct {
		etcdName    string
		namespace   string
		replicas    int32
		serviceName string
		want        []corev1.EnvVar
	}{
		"complete environment with 3 replicas": {
			etcdName:    "my-etcd",
			namespace:   "default",
			replicas:    3,
			serviceName: "my-etcd-headless",
			want: []corev1.EnvVar{
				{
					Name: "POD_NAME",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "metadata.name",
						},
					},
				},
				{
					Name: "POD_NAMESPACE",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "metadata.namespace",
						},
					},
				},
				{Name: "ETCD_NAME", Value: "$(POD_NAME)"},
				{Name: "ETCD_DATA_DIR", Value: "/var/lib/etcd"},
				{Name: "ETCD_LISTEN_CLIENT_URLS", Value: "http://0.0.0.0:2379"},
				{Name: "ETCD_LISTEN_PEER_URLS", Value: "http://0.0.0.0:2380"},
				{Name: "ETCD_ADVERTISE_CLIENT_URLS", Value: "http://$(POD_NAME).my-etcd-headless.$(POD_NAMESPACE).svc.cluster.local:2379"},
				{Name: "ETCD_INITIAL_ADVERTISE_PEER_URLS", Value: "http://$(POD_NAME).my-etcd-headless.$(POD_NAMESPACE).svc.cluster.local:2380"},
				{Name: "ETCD_INITIAL_CLUSTER_STATE", Value: "new"},
				{Name: "ETCD_INITIAL_CLUSTER_TOKEN", Value: "my-etcd"},
				{Name: "ETCD_INITIAL_CLUSTER", Value: "my-etcd-0=http://my-etcd-0.my-etcd-headless.default.svc.cluster.local:2380,my-etcd-1=http://my-etcd-1.my-etcd-headless.default.svc.cluster.local:2380,my-etcd-2=http://my-etcd-2.my-etcd-headless.default.svc.cluster.local:2380"},
			},
		},
		"single replica": {
			etcdName:    "test-etcd",
			namespace:   "test",
			replicas:    1,
			serviceName: "test-etcd-headless",
			want: []corev1.EnvVar{
				{
					Name: "POD_NAME",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "metadata.name",
						},
					},
				},
				{
					Name: "POD_NAMESPACE",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "metadata.namespace",
						},
					},
				},
				{Name: "ETCD_NAME", Value: "$(POD_NAME)"},
				{Name: "ETCD_DATA_DIR", Value: "/var/lib/etcd"},
				{Name: "ETCD_LISTEN_CLIENT_URLS", Value: "http://0.0.0.0:2379"},
				{Name: "ETCD_LISTEN_PEER_URLS", Value: "http://0.0.0.0:2380"},
				{Name: "ETCD_ADVERTISE_CLIENT_URLS", Value: "http://$(POD_NAME).test-etcd-headless.$(POD_NAMESPACE).svc.cluster.local:2379"},
				{Name: "ETCD_INITIAL_ADVERTISE_PEER_URLS", Value: "http://$(POD_NAME).test-etcd-headless.$(POD_NAMESPACE).svc.cluster.local:2380"},
				// Cluster setup won't happen in a single cluster, and these
				// env variables are only used at startup.
				{Name: "ETCD_INITIAL_CLUSTER_STATE", Value: "new"},
				{Name: "ETCD_INITIAL_CLUSTER_TOKEN", Value: "test-etcd"},
				{Name: "ETCD_INITIAL_CLUSTER", Value: "test-etcd-0=http://test-etcd-0.test-etcd-headless.test.svc.cluster.local:2380"},
			},
		},
		"zero replicas - no ETCD_INITIAL_CLUSTER": {
			etcdName:    "empty-etcd",
			namespace:   "default",
			replicas:    0,
			serviceName: "empty-etcd-headless",
			want: []corev1.EnvVar{
				{
					Name: "POD_NAME",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "metadata.name",
						},
					},
				},
				{
					Name: "POD_NAMESPACE",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "metadata.namespace",
						},
					},
				},
				{Name: "ETCD_NAME", Value: "$(POD_NAME)"},
				{Name: "ETCD_DATA_DIR", Value: "/var/lib/etcd"},
				{Name: "ETCD_LISTEN_CLIENT_URLS", Value: "http://0.0.0.0:2379"},
				{Name: "ETCD_LISTEN_PEER_URLS", Value: "http://0.0.0.0:2380"},
				{Name: "ETCD_ADVERTISE_CLIENT_URLS", Value: "http://$(POD_NAME).empty-etcd-headless.$(POD_NAMESPACE).svc.cluster.local:2379"},
				{Name: "ETCD_INITIAL_ADVERTISE_PEER_URLS", Value: "http://$(POD_NAME).empty-etcd-headless.$(POD_NAMESPACE).svc.cluster.local:2380"},
				// Cluster setup won't happen in a single cluster, and these
				// env variables are only used at startup. In case of scaling up
				// from zero replica, the updated env variable will be picked up
				// correctly, and thus an empty variable like this will be OK.
				{Name: "ETCD_INITIAL_CLUSTER_STATE", Value: "new"},
				{Name: "ETCD_INITIAL_CLUSTER_TOKEN", Value: "empty-etcd"},
				{Name: "ETCD_INITIAL_CLUSTER"},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := BuildEtcdEnv(tc.etcdName, tc.namespace, tc.replicas, tc.serviceName)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("BuildEtcdEnv() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
