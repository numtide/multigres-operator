//go:build e2e

package postgresconfig_test

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/multigres/multigres-operator/test/e2e/framework"
)

// TestLogConnectionsTakesEffect is a regression test for a postgresql.conf change
// that silently failed to apply on a running cluster: log_connections.
//
// log_connections is a superuser-backend context GUC: its value is fixed when a
// backend starts, and a SIGHUP reload only changes it for backends started
// afterwards — PostgreSQL never re-reads the config file mid-session. Here
// PostgreSQL sits behind the multipooler, which keeps long-lived pooled backends,
// so a reload reaches no running backend and the change has no observable effect
// (worse, the pooler's ReloadConfig reports the reload as applied, because
// pg_file_settings.applied is true for it, so the operator marks the change done).
//
// The operator must therefore treat log_connections as a restart-context change
// and recreate the pods — the only way the new value reaches the pooled backends.
// This test flips log_connections on against a running cluster and asserts the
// effective value read back through the gateway actually becomes "on".
func TestLogConnectionsTakesEffect(t *testing.T) {
	ns := cluster.CreateNamespace(t)
	c, err := cluster.CRClient()
	if err != nil {
		t.Fatalf("create CR client: %v", err)
	}
	ctx := context.Background()

	cr := framework.MustLoadCluster("config/samples/no-templates.yaml", ns)
	framework.WithCIResources(&cr.Spec)
	if err := c.Create(ctx, cr); err != nil {
		t.Fatalf("create MultigresCluster: %v", err)
	}

	// Wait for Postgres to come up and serve queries through the gateway.
	framework.WaitForPod(t, c, ns, "postgres")
	cluster.WaitForAllPodsReady(t, ns)
	gw := framework.FindGatewayService(t, cluster, ns)
	framework.WaitForQueryServing(t, cluster, ns, gw)

	// Baseline: log_connections defaults to off — the operator's baseline template
	// does not set it, so this proves the "on" below is our change taking effect.
	framework.WaitForPsqlValue(t, cluster, ns, gw, "SHOW log_connections", "off")

	poolPodUIDs := func() map[string]types.UID {
		pods := &corev1.PodList{}
		if err := c.List(ctx, pods, client.InNamespace(ns),
			client.MatchingLabels{"app.kubernetes.io/component": "shard-pool"}); err != nil {
			t.Fatalf("list pool pods: %v", err)
		}
		uids := map[string]types.UID{}
		for i := range pods.Items {
			uids[pods.Items[i].Name] = pods.Items[i].UID
		}
		return uids
	}

	// Change log_connections on a settled cluster so the rollout is not raced by
	// the initial config still converging.
	framework.WaitForShardConfigSettled(t, c, ns)
	before := poolPodUIDs()
	if len(before) == 0 {
		t.Fatal("no pool pods found before enabling log_connections")
	}

	// Turn log_connections on via the inline spec.postgresConfig map. Send the full
	// databases array with the one changed value: JSON merge-patch replaces arrays
	// wholesale, so mutate the live object to preserve every server-defaulted field.
	live := framework.GetCluster(t, c, ns, cr.Name)
	shard := &live.Spec.Databases[0].TableGroups[0].Shards[0]
	if shard.Spec.PostgresConfig == nil {
		shard.Spec.PostgresConfig = map[string]string{}
	}
	shard.Spec.PostgresConfig["log_connections"] = "on"
	framework.PatchCluster(t, c, live, framework.MustMarshal(map[string]any{
		"spec": map[string]any{"databases": live.Spec.Databases},
	}))

	// The change must take effect in the running server. Reading it back through
	// the gateway (a pooled backend) is exactly what a user does — and is the
	// assertion that fails when the change is applied by SIGHUP reload only,
	// because the pooled backend keeps its start-time value.
	framework.WaitForPsqlValue(t, cluster, ns, gw, "SHOW log_connections", "on")

	// It takes effect because the pods were recreated: a backend-context GUC that a
	// reload cannot apply to existing pooled backends goes through the restart
	// path. Changed pool-pod UIDs prove Postgres was actually restarted.
	after := poolPodUIDs()
	recreated := false
	for name, uid := range before {
		if after[name] != uid {
			recreated = true
			break
		}
	}
	if !recreated {
		t.Errorf("expected pool pods to be recreated to apply log_connections, but UIDs were unchanged: before=%v after=%v", before, after)
	}
}
