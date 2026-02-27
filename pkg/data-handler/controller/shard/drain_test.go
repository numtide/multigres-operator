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

func (m *mockRPCClient) BeginTerm(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *consensusdatapb.BeginTermRequest,
) (*consensusdatapb.BeginTermResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) ConsensusStatus(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *consensusdatapb.StatusRequest,
) (*consensusdatapb.StatusResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) GetLeadershipView(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *consensusdatapb.LeadershipViewRequest,
) (*consensusdatapb.LeadershipViewResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) CanReachPrimary(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *consensusdatapb.CanReachPrimaryRequest,
) (*consensusdatapb.CanReachPrimaryResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) InitializeEmptyPrimary(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.InitializeEmptyPrimaryRequest,
) (*multipoolermanagerdatapb.InitializeEmptyPrimaryResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) State(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.StateRequest,
) (*multipoolermanagerdatapb.StateResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) WaitForLSN(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.WaitForLSNRequest,
) (*multipoolermanagerdatapb.WaitForLSNResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) SetPrimaryConnInfo(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.SetPrimaryConnInfoRequest,
) (*multipoolermanagerdatapb.SetPrimaryConnInfoResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) StartReplication(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.StartReplicationRequest,
) (*multipoolermanagerdatapb.StartReplicationResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) StopReplication(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.StopReplicationRequest,
) (*multipoolermanagerdatapb.StopReplicationResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) StandbyReplicationStatus(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.StandbyReplicationStatusRequest,
) (*multipoolermanagerdatapb.StandbyReplicationStatusResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) Status(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.StatusRequest,
) (*multipoolermanagerdatapb.StatusResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) ResetReplication(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.ResetReplicationRequest,
) (*multipoolermanagerdatapb.ResetReplicationResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) StopReplicationAndGetStatus(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.StopReplicationAndGetStatusRequest,
) (*multipoolermanagerdatapb.StopReplicationAndGetStatusResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) ConfigureSynchronousReplication(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.ConfigureSynchronousReplicationRequest,
) (*multipoolermanagerdatapb.ConfigureSynchronousReplicationResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) PrimaryStatus(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.PrimaryStatusRequest,
) (*multipoolermanagerdatapb.PrimaryStatusResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) PrimaryPosition(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.PrimaryPositionRequest,
) (*multipoolermanagerdatapb.PrimaryPositionResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) GetFollowers(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.GetFollowersRequest,
) (*multipoolermanagerdatapb.GetFollowersResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) EmergencyDemote(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.EmergencyDemoteRequest,
) (*multipoolermanagerdatapb.EmergencyDemoteResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) UndoDemote(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.UndoDemoteRequest,
) (*multipoolermanagerdatapb.UndoDemoteResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) DemoteStalePrimary(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.DemoteStalePrimaryRequest,
) (*multipoolermanagerdatapb.DemoteStalePrimaryResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) ChangeType(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.ChangeTypeRequest,
) (*multipoolermanagerdatapb.ChangeTypeResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) GetDurabilityPolicy(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.GetDurabilityPolicyRequest,
) (*multipoolermanagerdatapb.GetDurabilityPolicyResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) CreateDurabilityPolicy(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.CreateDurabilityPolicyRequest,
) (*multipoolermanagerdatapb.CreateDurabilityPolicyResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) Backup(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.BackupRequest,
) (*multipoolermanagerdatapb.BackupResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) RestoreFromBackup(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.RestoreFromBackupRequest,
) (*multipoolermanagerdatapb.RestoreFromBackupResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) GetBackups(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.GetBackupsRequest,
) (*multipoolermanagerdatapb.GetBackupsResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) GetBackupByJobId(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.GetBackupByJobIdRequest,
) (*multipoolermanagerdatapb.GetBackupByJobIdResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) RewindToSource(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.RewindToSourceRequest,
) (*multipoolermanagerdatapb.RewindToSourceResponse, error) {
	return nil, nil
}

func (m *mockRPCClient) SetMonitor(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.SetMonitorRequest,
) (*multipoolermanagerdatapb.SetMonitorResponse, error) {
	return nil, nil
}
func (m *mockRPCClient) Close()                                          {}
func (m *mockRPCClient) CloseTablet(pooler *clustermetadata.MultiPooler) {}

func (m *mockRPCClient) Promote(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.PromoteRequest,
) (*multipoolermanagerdatapb.PromoteResponse, error) {
	m.promoteCalled = true
	return &multipoolermanagerdatapb.PromoteResponse{}, nil
}

func (m *mockRPCClient) UpdateSynchronousStandbyList(
	ctx context.Context,
	pooler *clustermetadata.MultiPooler,
	request *multipoolermanagerdatapb.UpdateSynchronousStandbyListRequest,
) (*multipoolermanagerdatapb.UpdateSynchronousStandbyListResponse, error) {
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
			DatabaseName:     "test-db",
			TableGroupName:   "test-tg",
			ShardName:        "0",
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

	primaryPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "primary-pod",
			Namespace: "default",
			Labels: map[string]string{
				metadata.LabelMultigresCell: "cell1",
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(shardObj, pod, primaryPod).Build()
	rpcMock := &mockRPCClient{}

	reconciler := &ShardReconciler{
		Client:    c,
		Scheme:    scheme,
		Recorder:  record.NewFakeRecorder(10),
		rpcClient: rpcMock,
	}

	_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")

	reconciler.createTopoStore = func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
		return topoclient.NewWithFactory(
			factory,
			"",
			[]string{""},
			topoclient.NewDefaultTopoConfig(),
		), nil
	}

	ctx := context.Background()
	store := topoclient.NewWithFactory(factory, "", []string{""}, topoclient.NewDefaultTopoConfig())
	defer func() { _ = store.Close() }()

	// Add primary
	_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
		Id:         &clustermetadata.ID{Cell: "cell1", Name: "primary-pod"},
		Hostname:   "primary-pod",
		Type:       clustermetadata.PoolerType_PRIMARY,
		Database:   "test-db",
		TableGroup: "test-tg",
		Shard:      "0",
	}, false)
	// Add our replica pod
	_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
		Id:         &clustermetadata.ID{Cell: "cell1", Name: "test-pod-0"},
		Hostname:   "test-pod-0",
		Type:       clustermetadata.PoolerType_REPLICA,
		Database:   "test-db",
		TableGroup: "test-tg",
		Shard:      "0",
	}, false)

	// Step 1: Requested -> Draining
	requeue, err := reconciler.executeDrainStateMachine(ctx, store, shardObj, pod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !requeue {
		t.Fatalf("expected requeue for replica state transition")
	}

	_ = c.Get(ctx, client.ObjectKeyFromObject(pod), pod)
	if pod.Annotations[metadata.AnnotationDrainState] != metadata.DrainStateDraining {
		t.Fatalf("expected state draining, got %v", pod.Annotations[metadata.AnnotationDrainState])
	}
	if !rpcMock.updateStandbyCalled {
		t.Fatalf("expected UpdateSynchronousStandbyList to be called")
	}

	// Step 2: Draining -> Acknowledged
	_, _ = reconciler.executeDrainStateMachine(ctx, store, shardObj, pod)
	_ = c.Get(ctx, client.ObjectKeyFromObject(pod), pod)
	if pod.Annotations[metadata.AnnotationDrainState] != metadata.DrainStateAcknowledged {
		t.Fatalf(
			"expected state acknowledged, got %v",
			pod.Annotations[metadata.AnnotationDrainState],
		)
	}

	// Step 3: Acknowledged -> ReadyForDeletion
	_, _ = reconciler.executeDrainStateMachine(ctx, store, shardObj, pod)
	_ = c.Get(ctx, client.ObjectKeyFromObject(pod), pod)
	if pod.Annotations[metadata.AnnotationDrainState] != metadata.DrainStateReadyForDeletion {
		t.Fatalf(
			"expected state ready-for-deletion, got %v",
			pod.Annotations[metadata.AnnotationDrainState],
		)
	}

	inspectorStore := topoclient.NewWithFactory(
		factory,
		"",
		[]string{""},
		topoclient.NewDefaultTopoConfig(),
	)
	defer func() { _ = inspectorStore.Close() }()
	poolers, _ := inspectorStore.GetMultiPoolersByCell(ctx, "cell1", nil)
	if len(poolers) != 1 || poolers[0].GetHostname() != "primary-pod" {
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
			DatabaseName:     "test-db",
			TableGroupName:   "test-tg",
			ShardName:        "0",
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
			Labels:    map[string]string{metadata.LabelMultigresCell: "cell1"},
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

	_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")

	reconciler.createTopoStore = func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
		return topoclient.NewWithFactory(
			factory,
			"",
			[]string{""},
			topoclient.NewDefaultTopoConfig(),
		), nil
	}

	ctx := context.Background()
	store := topoclient.NewWithFactory(factory, "", []string{""}, topoclient.NewDefaultTopoConfig())
	defer func() { _ = store.Close() }()

	_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
		Id:       &clustermetadata.ID{Cell: "cell1", Name: "test-pod-0"},
		Hostname: "test-pod-0", Type: clustermetadata.PoolerType_PRIMARY,
		Database:   "test-db",
		TableGroup: "test-tg",
		Shard:      "0",
	}, false)
	_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
		Id:       &clustermetadata.ID{Cell: "cell1", Name: "replica-pod"},
		Hostname: "replica-pod", Type: clustermetadata.PoolerType_REPLICA,
		Database:   "test-db",
		TableGroup: "test-tg",
		Shard:      "0",
	}, false)

	// PRIMARY drain should advance to DrainStateDraining without calling Promote.
	// Failover is multiorch's responsibility via its consensus protocol.
	requeue, err := reconciler.executeDrainStateMachine(ctx, store, shardObj, pod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !requeue {
		t.Fatalf("expected requeue after state transition")
	}

	_ = c.Get(ctx, client.ObjectKeyFromObject(pod), pod)
	if pod.Annotations[metadata.AnnotationDrainState] != metadata.DrainStateDraining {
		t.Fatalf("expected PRIMARY pod to advance to draining, got %v",
			pod.Annotations[metadata.AnnotationDrainState])
	}
	if rpcMock.promoteCalled {
		t.Fatalf("Promote should not be called; failover is multiorch's responsibility")
	}
	if rpcMock.updateStandbyCalled {
		t.Fatalf("UpdateSynchronousStandbyList should not be called when draining the PRIMARY")
	}
}

func TestPrimaryDrainFlowNilRPCClient(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shardObj := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-shard", Namespace: "default",
			Labels: map[string]string{metadata.LabelMultigresCluster: "test"},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:     "test-db",
			TableGroupName:   "test-tg",
			ShardName:        "0",
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
			Labels:    map[string]string{metadata.LabelMultigresCell: "cell1"},
			Annotations: map[string]string{
				metadata.AnnotationDrainState: metadata.DrainStateRequested,
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(shardObj, pod).Build()
	reconciler := &ShardReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
		// rpcClient intentionally nil
	}

	_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")

	reconciler.createTopoStore = func(s *multigresv1alpha1.Shard) (topoclient.Store, error) {
		return topoclient.NewWithFactory(
			factory,
			"",
			[]string{""},
			topoclient.NewDefaultTopoConfig(),
		), nil
	}

	ctx := context.Background()
	store := topoclient.NewWithFactory(factory, "", []string{""}, topoclient.NewDefaultTopoConfig())
	defer func() { _ = store.Close() }()

	_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
		Id:       &clustermetadata.ID{Cell: "cell1", Name: "test-pod-0"},
		Hostname: "test-pod-0", Type: clustermetadata.PoolerType_PRIMARY,
		Database:   "test-db",
		TableGroup: "test-tg",
		Shard:      "0",
	}, false)
	_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
		Id:       &clustermetadata.ID{Cell: "cell1", Name: "replica-pod"},
		Hostname: "replica-pod", Type: clustermetadata.PoolerType_REPLICA,
		Database:   "test-db",
		TableGroup: "test-tg",
		Shard:      "0",
	}, false)

	// With nil rpcClient, drain should proceed instead of looping forever
	requeue, err := reconciler.executeDrainStateMachine(ctx, store, shardObj, pod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !requeue {
		t.Fatalf("expected requeue after state transition")
	}

	_ = c.Get(ctx, client.ObjectKeyFromObject(pod), pod)
	if pod.Annotations[metadata.AnnotationDrainState] != metadata.DrainStateDraining {
		t.Fatalf(
			"expected PRIMARY pod to advance to draining when rpcClient is nil, got %v",
			pod.Annotations[metadata.AnnotationDrainState],
		)
	}
}

func TestStuckTerminatingPod(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shardObj := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-shard", Namespace: "default",
			Labels: map[string]string{metadata.LabelMultigresCluster: "test"},
		},
		Spec: multigresv1alpha1.ShardSpec{
			DatabaseName:     "test-db",
			TableGroupName:   "test-tg",
			ShardName:        "0",
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
			Labels:    map[string]string{metadata.LabelMultigresCell: "cell1"},
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

	store, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")
	defer func() { _ = store.Close() }()

	reconciler.createTopoStore = func(s *multigresv1alpha1.Shard) (topoclient.Store, error) { return store, nil }

	ctx := context.Background()
	_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
		Id:       &clustermetadata.ID{Cell: "cell1", Name: "test-pod-0"},
		Hostname: "test-pod-0", Type: clustermetadata.PoolerType_REPLICA,
		Database:   "test-db",
		TableGroup: "test-tg",
		Shard:      "0",
	}, false)

	// Delete the pod using the client to set DeletionTimestamp
	_ = c.Delete(ctx, pod)

	// Fast forward DeletionTimestamp by 10 minutes
	_ = c.Get(ctx, client.ObjectKeyFromObject(pod), pod)
	delTime := metav1.NewTime(time.Now().Add(-10 * time.Minute))
	pod.DeletionTimestamp = &delTime

	_, _ = reconciler.executeDrainStateMachine(ctx, store, shardObj, pod)
	// Recreate a new inspector store to verify unregistration without reuse
	inspectorStore := topoclient.NewWithFactory(
		factory,
		"",
		[]string{""},
		topoclient.NewDefaultTopoConfig(),
	)
	defer func() { _ = inspectorStore.Close() }()

	poolers, _ := inspectorStore.GetMultiPoolersByCell(ctx, "cell1", nil)
	if len(poolers) != 0 {
		t.Fatalf("expected stuck pod to be immediately unregistered")
	}
}

func TestIsPrimaryTerminatingOrMissing(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = multigresv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
		},
	}

	t.Run("returns true for nil primary", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &ShardReconciler{Client: c}
		if !r.isPrimaryTerminatingOrMissing(context.Background(), shard, nil) {
			t.Error("Expected true for nil primary")
		}
	})

	t.Run("returns false for primary without drain annotation", func(t *testing.T) {
		primaryPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "primary-pod",
				Namespace: "default",
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(primaryPod).Build()
		r := &ShardReconciler{Client: c}

		primary := &clustermetadata.MultiPooler{
			Id: &clustermetadata.ID{Cell: "cell1", Name: "primary-pod"},
		}
		if r.isPrimaryTerminatingOrMissing(context.Background(), shard, primary) {
			t.Error("Expected false for primary without drain annotation")
		}
	})

	t.Run("returns false when primary pod not found", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &ShardReconciler{Client: c}

		primary := &clustermetadata.MultiPooler{
			Id: &clustermetadata.ID{Cell: "cell1", Name: "nonexistent-pod"},
		}
		if !r.isPrimaryTerminatingOrMissing(context.Background(), shard, primary) {
			t.Error("Expected true when primary pod not found")
		}
	})
}

func TestReplicaDrain_SkipsRPCWhenPrimaryDraining(t *testing.T) {
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
			DatabaseName:     "test-db",
			TableGroupName:   "test-tg",
			ShardName:        "0",
			GlobalTopoServer: multigresv1alpha1.GlobalTopoServerRef{RootPath: "/test"},
			Pools: map[multigresv1alpha1.PoolName]multigresv1alpha1.PoolSpec{
				"pool1": {Cells: []multigresv1alpha1.CellName{"cell1"}},
			},
		},
	}

	// The primary pod has a drain annotation — it's being drained too
	primaryPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "primary-pod",
			Namespace: "default",
			Annotations: map[string]string{
				metadata.AnnotationDrainState: metadata.DrainStateDraining,
			},
		},
	}

	replicaPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "replica-pod-0",
			Namespace: "default",
			Labels: map[string]string{
				metadata.LabelMultigresCell: "cell1",
			},
			Annotations: map[string]string{
				metadata.AnnotationDrainState: metadata.DrainStateRequested,
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(shardObj, primaryPod, replicaPod).
		Build()
	rpcMock := &mockRPCClient{}

	reconciler := &ShardReconciler{
		Client:    c,
		Scheme:    scheme,
		Recorder:  record.NewFakeRecorder(10),
		rpcClient: rpcMock,
	}

	_, factory := memorytopo.NewServerAndFactory(context.Background(), "cell1")
	store := topoclient.NewWithFactory(factory, "", []string{""}, topoclient.NewDefaultTopoConfig())
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	// Register primary and replica in topo
	_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
		Id:         &clustermetadata.ID{Cell: "cell1", Name: "primary-pod"},
		Hostname:   "primary-pod",
		Type:       clustermetadata.PoolerType_PRIMARY,
		Database:   "test-db",
		TableGroup: "test-tg",
		Shard:      "0",
	}, false)
	_ = store.RegisterMultiPooler(ctx, &clustermetadata.MultiPooler{
		Id:         &clustermetadata.ID{Cell: "cell1", Name: "replica-pod-0"},
		Hostname:   "replica-pod-0",
		Type:       clustermetadata.PoolerType_REPLICA,
		Database:   "test-db",
		TableGroup: "test-tg",
		Shard:      "0",
	}, false)

	// Execute drain for replica while primary is draining
	requeue, err := reconciler.executeDrainStateMachine(ctx, store, shardObj, replicaPod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !requeue {
		t.Error("Expected requeue when primary is draining")
	}

	// The RPC should NOT have been called because primary is draining
	if rpcMock.updateStandbyCalled {
		t.Error("UpdateSynchronousStandbyList should NOT be called when primary is draining")
	}

	// The drain state should NOT have advanced (still "requested")
	updatedPod := &corev1.Pod{}
	_ = c.Get(ctx, client.ObjectKeyFromObject(replicaPod), updatedPod)
	if updatedPod.Annotations[metadata.AnnotationDrainState] != metadata.DrainStateRequested {
		t.Errorf("Expected drain state to remain %q, got %q",
			metadata.DrainStateRequested,
			updatedPod.Annotations[metadata.AnnotationDrainState])
	}
}
