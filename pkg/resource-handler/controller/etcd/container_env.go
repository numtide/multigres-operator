package etcd

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// buildContainerEnv constructs all environment variables for etcd clustering in
// StatefulSets. This combines pod identity, etcd config, and cluster peer
// discovery details.
func buildContainerEnv(
	etcdName, namespace string,
	replicas int32,
	serviceName string,
) []corev1.EnvVar {
	envVars := make([]corev1.EnvVar, 0)

	// Add pod identity variables from downward API
	envVars = append(envVars, buildPodIdentityEnv()...)

	// Add etcd configuration variables
	envVars = append(envVars, buildEtcdConfigEnv(etcdName, serviceName, namespace)...)

	// Add the initial cluster peer list
	clusterPeerList := buildEtcdClusterPeerList(etcdName, serviceName, namespace, replicas)
	envVars = append(envVars, corev1.EnvVar{
		Name:  "ETCD_INITIAL_CLUSTER",
		Value: clusterPeerList,
	})

	return envVars
}

// buildPodIdentityEnv creates environment variables for pod name and namespace.
// These are required for etcd to construct its advertise URLs in StatefulSets,
// and this association of both Pod name and namespace are common.
//
// Ref: https://etcd.io/docs/latest/op-guide/clustering/
func buildPodIdentityEnv() []corev1.EnvVar {
	return []corev1.EnvVar{
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
}

// buildEtcdConfigEnv creates etcd configuration environment variables.
// These configure etcd's network endpoints and cluster formation.
//
// Ref: https://etcd.io/docs/latest/op-guide/configuration/
func buildEtcdConfigEnv(etcdName, serviceName, namespace string) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  "ETCD_NAME",
			Value: "$(POD_NAME)",
		},
		{
			Name:  "ETCD_DATA_DIR",
			Value: "/var/lib/etcd",
		},
		{
			Name:  "ETCD_LISTEN_CLIENT_URLS",
			Value: "http://0.0.0.0:2379",
		},
		{
			Name:  "ETCD_LISTEN_PEER_URLS",
			Value: "http://0.0.0.0:2380",
		},
		{
			Name: "ETCD_ADVERTISE_CLIENT_URLS",
			Value: fmt.Sprintf(
				"http://$(POD_NAME).%s.$(POD_NAMESPACE).svc.cluster.local:2379",
				serviceName,
			),
		},
		{
			Name: "ETCD_INITIAL_ADVERTISE_PEER_URLS",
			Value: fmt.Sprintf(
				"http://$(POD_NAME).%s.$(POD_NAMESPACE).svc.cluster.local:2380",
				serviceName,
			),
		},
		{
			Name:  "ETCD_INITIAL_CLUSTER_STATE",
			Value: "new",
		},
		{
			Name:  "ETCD_INITIAL_CLUSTER_TOKEN",
			Value: etcdName,
		},
	}
}

// buildEtcdClusterPeerList generates the initial cluster member list for
// bootstrap. This tells each etcd member about all other members during cluster
// formation.
//
// Format: member-0=http://member-0.service.ns.svc.cluster.local:2380,...
//
// Ref: https://etcd.io/docs/latest/op-guide/clustering/#static
func buildEtcdClusterPeerList(etcdName, serviceName, namespace string, replicas int32) string {
	if replicas < 0 {
		return ""
	}

	peers := make([]string, 0, replicas)
	for i := range replicas {
		podName := fmt.Sprintf("%s-%d", etcdName, i)
		peerURL := fmt.Sprintf("%s=http://%s.%s.%s.svc.cluster.local:2380",
			podName, podName, serviceName, namespace)
		peers = append(peers, peerURL)
	}

	return strings.Join(peers, ",")
}
