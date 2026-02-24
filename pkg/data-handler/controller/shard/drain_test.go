package shard

import (
	"context"
	"testing"
	"time"

	"github.com/multigres/multigres/go/common/topoclient"
	"github.com/multigres/multigres/go/common/topoclient/memorytopo"
	"github.com/multigres/multigres/go/pb/clustermetadata"
	consensusdatapb "github.com/multigres/multigres/go/pb/consensusdata"
	multipoolermanagerdatapb "github.com/multigres/multigres/go/pb/multipoolermanagerdata"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/util/metadata"
)

// mockRPCClient implements just the methods we need
type mockRPCClient struct {
	promoteCalled       bool
	updateStandbyCalled bool
}

func (m *mockRPCClient) BeginTerm(ctx context.Context, pooler *clustermetadata.MultiPooler, request *consensusdatapb.BeginTermRequest) (*consensusdatapb.BeginTermResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) ConsensusStatus(ctx context.Context, pooler *clustermetadata.MultiPooler, request *consensusdatapb.StatusRequest) (*consensusdatapb.StatusResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) GetLeadershipView(ctx context.Context, pooler *clustermetadata.MultiPooler, request *consensusdatapb.LeadershipViewRequest) (*consensusdatapb.LeadershipViewResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) CanReachPrimary(ctx context.Context, pooler *clustermetadata.MultiPooler, request *consensusdatapb.CanReachPrimaryRequest) (*consensusdatapb.CanReachPrimaryResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) InitializeEmptyPrimary(ctx context.Context, pooler *clustermetadata.MultiPooler, request *multipoolermanagerdatapb.InitializeEmptyPrimaryRequest) (*multipoolermanagerdatapb.InitializeEmptyPrimaryResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) State(ctx context.Context, pooler *clustermetadata.MultiPooler, request *multipoolermanagerdatapb.StateRequest) (*multipoolermanagerdatapb.StateResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) WaitForLSN(ctx context.Context, pooler *clustermetadata.MultiPooler, request *multipoolermanagerdatapb.WaitForLSNRequest) (*multipoolermanagerdatapb.WaitForLSNResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) SetPrimaryConnInfo(ctx context.Context, pooler *clustermetadata.MultiPooler, request *multipoolermanagerdatapb.SetPrimaryConnInfoRequest) (*multipoolermanagerdatapb.SetPrimaryConnInfoResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) StartReplication(ctx context.Context, pooler *clustermetadata.MultiPooler, request *multipoolermanagerdatapb.StartReplicationRequest) (*multipoolermanagerdatapb.StartReplicationResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) StopReplication(ctx context.Context, pooler *clustermetadata.MultiPooler, request *multipoolermanagerdatapb.StopReplicationRequest) (*multipoolermanagerdatapb.StopReplicationResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) StandbyReplicationStatus(ctx context.Context, pooler *clustermetadata.MultiPooler, request *multipoolermanagerdatapb.StandbyReplicationStatusRequest) (*multipoolermanagerdatapb.StandbyReplicationStatusResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) Status(ctx context.Context, pooler *clustermetadata.MultiPooler, request *multipoolermanagerdatapb.StatusRequest) (*multipoolermanagerdatapb.StatusResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) ResetReplication(ctx context.Context, pooler *clustermetadata.MultiPooler, request *multipoolermanagerdatapb.ResetReplicationRequest) (*multipoolermanagerdatapb.ResetReplicationResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) StopReplicationAndGetStatus(ctx context.Context, pooler *clustermetadata.MultiPooler, request *multipoolermanagerdatapb.StopReplicationAndGetStatusRequest) (*multipoolermanagerdatapb.StopReplicationAndGetStatusResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) ConfigureSynchronousReplication(ctx context.Context, pooler *clustermetadata.MultiPooler, request *multipoolermanagerdatapb.ConfigureSynchronousReplicationRequest) (*multipoolermanagerdatapb.ConfigureSynchronousReplicationResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) PrimaryStatus(ctx context.Context, pooler *clustermetadata.MultiPooler, request *multipoolermanagerdatapb.PrimaryStatusRequest) (*multipoolermanagerdatapb.PrimaryStatusResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) PrimaryPosition(ctx context.Context, pooler *clustermetadata.MultiPooler, request *multipoolermanagerdatapb.PrimaryPositionRequest) (*multipoolermanagerdatapb.PrimaryPositionResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) GetFollowers(ctx context.Context, pooler *clustermetadata.MultiPooler, request *multipoolermanagerdatapb.GetFollowersRequest) (*multipoolermanagerdatapb.GetFollowersResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) EmergencyDemote(ctx context.Context, pooler *clustermetadata.MultiPooler, request *multipoolermanagerdatapb.EmergencyDemoteRequest) (*multipoolermanagerdatapb.EmergencyDemoteResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) UndoDemote(ctx context.Context, pooler *clustermetadata.MultiPooler, request *multipoolermanagerdatapb.UndoDemoteRequest) (*multipoolermanagerdatapb.UndoDemoteResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) DemoteStalePrimary(ctx context.Context, pooler *clustermetadata.MultiPooler, request *multipoolermanagerdatapb.DemoteStalePrimaryRequest) (*multipoolermanagerdatapb.DemoteStalePrimaryResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) ChangeType(ctx context.Context, pooler *clustermetadata.MultiPooler, request *multipoolermanagerdatapb.ChangeTypeRequest) (*multipoolermanagerdatapb.ChangeTypeResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) GetDurabilityPolicy(ctx context.Context, pooler *clustermetadata.MultiPooler, request *multipoolermanagerdatapb.GetDurabilityPolicyRequest) (*multipoolermanagerdatapb.GetDurabilityPolicyResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) CreateDurabilityPolicy(ctx context.Context, pooler *clustermetadata.MultiPooler, request *multipoolermanagerdatapb.CreateDurabilityPolicyRequest) (*multipoolermanagerdatapb.CreateDurabilityPolicyResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) Backup(ctx context.Context, pooler *clustermetadata.MultiPooler, request *multipoolermanagerdatapb.BackupRequest) (*multipoolermanagerdatapb.BackupResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) RestoreFromBackup(ctx context.Context, pooler *clustermetadata.MultiPooler, request *multipoolermanagerdatapb.RestoreFromBackupRequest) (*multipoolermanagerdatapb.RestoreFromBackupResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) GetBackups(ctx context.Context, pooler *clustermetadata.MultiPooler, request *multipoolermanagerdatapb.GetBackupsRequest) (*multipoolermanagerdatapb.GetBackupsResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) GetBackupByJobId(ctx context.Context, pooler *clustermetadata.MultiPooler, request *multipoolermanagerdatapb.GetBackupByJobIdRequest) (*multipoolermanagerdatapb.GetBackupByJobIdResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) RewindToSource(ctx context.Context, pooler *clustermetadata.MultiPooler, request *multipoolermanagerdatapb.RewindToSourceRequest) (*multipoolermanagerdatapb.RewindToSourceResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) SetMonitor(ctx context.Context, pooler *clustermetadata.MultiPooler, request *multipoolermanagerdatapb.SetMonitorRequest) (*multipoolermanagerdatapb.SetMonitorResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) Close()                                          {}
func (m *mockRPCClient) CloseTablet(pooler *clustermetadata.MultiPooler) {}

func (m *mockRPCClient) Promote(ctx context.Context, pooler *clustermetadata.MultiPooler, request *multipoolermanagerdatapb.PromoteRequest) (*multipoolermanagerdatapb.PromoteResponse, error) {
	m.promoteCalled = true
	return &multipoolermanagerdatapb.PromoteResponse{}, nil
}

func (m *mockRPCClient) UpdateSynchronousStandbyList(ctx context.Context, pooler *clustermetadata.MultiPooler, request *multipoolermanagerdatapb.UpdateSynchronousStandbyListRequest) (*multipoolermanagerdatapb.UpdateSynchronousStandbyListResponse, error) {
	m.updateStandbyCalled = true
	return &multipoolermanagerdatapb.UpdateSynchronousStandbyListResponse{}, nil
}

func TestReplicaDrainFlow(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shardObj := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
			Labels: map[string]string{
				metadata.LabelMultigresCluster: "test-cluster",
			},
		},
		Spec: multigresv1alpha1.ShardSpec{
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{RootPath: "/test"},
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"pool1": {Cells: []multigresv1alpha1.CellName{"cell1"}},
			},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-0",
			Namespace: "default",
			Labels: map[string]string{
				metadata.LabelMultigresCell: "cell1",
			},
			Annotations: map[string]string{
				metadata.AnnotationDrainState: metadata.DrainStateRequested,
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(shardObj, pod).Build()
	rpcMock := &mockRPCClient{}

	reconciler := &ShardReconciler{
		Client:    c,
		Scheme:    scheme,
		Recorder:  record.NewFakeRecorder(10),
		rpcClient: rpcMock,
	}

	store, _ := memorytopo.NewServerAndFactory(context.Background())
	defer store.Close()
	reconciler.createTopoStore = func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
		return store, nil
	}

	ctx := context.Background()
	// Add primary
	store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
		Id:       &clustermetadata.ID{Cell: "cell1", Name: "primary-pod"},
		Hostname: "primary-pod",
		Type:     clustermetadata.PoolerType_PRIMARY,
	}, false)
	// Add our replica pod
	store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
		Id:       &clustermetadata.ID{Cell: "cell1", Name: "test-pod-0"},
		Hostname: "test-pod-0",
		Type:     clustermetadata.PoolerType_REPLICA,
	}, false)

	// Step 1: Requested -> Draining
	requeue, err := reconciler.executeDrainStateMachine(ctx, shardObj, pod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requeue {
		t.Fatalf("expected no requeue for replica")
	}

	c.Get(ctx, client.ObjectKeyFromObject(pod), pod)
	if pod.Annotations[metadata.AnnotationDrainState] != metadata.DrainStateDraining {
		t.Fatalf("expected state draining, got %v", pod.Annotations[metadata.AnnotationDrainState])
	}
	if !rpcMock.updateStandbyCalled {
		t.Fatalf("expected UpdateSynchronousStandbyList to be called")
	}

	// Step 2: Draining -> Acknowledged
	reconciler.executeDrainStateMachine(ctx, shardObj, pod)
	c.Get(ctx, client.ObjectKeyFromObject(pod), pod)
	if pod.Annotations[metadata.AnnotationDrainState] != metadata.DrainStateAcknowledged {
		t.Fatalf("expected state acknowledged, got %v", pod.Annotations[metadata.AnnotationDrainState])
	}

	// Step 3: Acknowledged -> ReadyForDeletion
	reconciler.executeDrainStateMachine(ctx, shardObj, pod)
	c.Get(ctx, client.ObjectKeyFromObject(pod), pod)
	if pod.Annotations[metadata.AnnotationDrainState] != metadata.DrainStateReadyForDeletion {
		t.Fatalf("expected state ready-for-deletion, got %v", pod.Annotations[metadata.AnnotationDrainState])
	}

	poolers, _ := store.GetMultiPoolersByCell(ctx, "cell1", nil)
	if len(poolers) != 1 || poolers[0].MultiPooler.GetHostname() != "primary-pod" {
		t.Fatalf("expected replica to be unregistered from topo")
	}
}

func TestPrimaryDrainFlow(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shardObj := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-shard", Namespace: "default",
			Labels: map[string]string{metadata.LabelMultigresCluster: "test"},
		},
		Spec: multigresv1alpha1.ShardSpec{
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{RootPath: "/test"},
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"pool1": {Cells: []multigresv1alpha1.CellName{"cell1"}},
			},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod-0", Namespace: "default",
			Labels:      map[string]string{metadata.LabelMultigresCell: "cell1"},
			Annotations: map[string]string{metadata.AnnotationDrainState: metadata.DrainStateRequested},
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(shardObj, pod).Build()
	rpcMock := &mockRPCClient{}
	reconciler := &ShardReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10), rpcClient: rpcMock}

	store, _ := memorytopo.NewServerAndFactory(context.Background())
	defer store.Close()
	reconciler.createTopoStore = func(s *multigresv1alpha1.Shard) (topoclient.Store, error) { return store, nil }

	ctx := context.Background()
	store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
		Id:       &clustermetadata.ID{Cell: "cell1", Name: "test-pod-0"},
		Hostname: "test-pod-0", Type: clustermetadata.PoolerType_PRIMARY,
	}, false)
	store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
		Id:       &clustermetadata.ID{Cell: "cell1", Name: "replica-pod"},
		Hostname: "replica-pod", Type: clustermetadata.PoolerType_REPLICA,
	}, false)

	requeue, err := reconciler.executeDrainStateMachine(ctx, shardObj, pod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !requeue {
		t.Fatalf("expected requeue for primary to wait for role change")
	}
	if !rpcMock.promoteCalled {
		t.Fatalf("expected promote to be called on another replica")
	}
}

func TestStuckTerminatingPod(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shardObj := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{Name: "test-shard", Namespace: "default"},
	}

	// Terminating for 10 minutes
	delTime := metav1.NewTime(time.Now().Add(-10 * time.Minute))

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod-0", Namespace: "default",
			Labels:            map[string]string{metadata.LabelMultigresCell: "cell1"},
			Annotations:       map[string]string{metadata.AnnotationDrainState: metadata.DrainStateRequested},
			DeletionTimestamp: &delTime,
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(shardObj, pod).Build()
	reconciler := &ShardReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10)}

	store, _ := memorytopo.NewServerAndFactory(context.Background())
	defer store.Close()
	reconciler.createTopoStore = func(s *multigresv1alpha1.Shard) (topoclient.Store, error) { return store, nil }

	ctx := context.Background()
	store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
		Id:       &clustermetadata.ID{Cell: "cell1", Name: "test-pod-0"},
		Hostname: "test-pod-0", Type: clustermetadata.PoolerType_REPLICA,
	}, false)

	reconciler.executeDrainStateMachine(ctx, shardObj, pod)

	poolers, _ := store.GetMultiPoolersByCell(ctx, "cell1", nil)
	if len(poolers) != 0 {
		t.Fatalf("expected stuck pod to be immediately unregistered")
	}
}
