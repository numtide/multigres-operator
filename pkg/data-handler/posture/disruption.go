package posture

import (
	"context"
	"fmt"

	"github.com/multigres/multigres/go/common/consensus"
	"github.com/multigres/multigres/go/common/rpcclient"
	"github.com/multigres/multigres/go/common/topoclient"
	clustermetadatapb "github.com/multigres/multigres/go/pb/clustermetadata"
	multipoolermanagerdatapb "github.com/multigres/multigres/go/pb/multipoolermanagerdata"
	"google.golang.org/protobuf/proto"

	multigresv1alpha1 "github.com/multigres/multigres-operator/api/v1alpha1"
	"github.com/multigres/multigres-operator/pkg/data-handler/topo"
)

// CheckDisruption observes the data plane afresh before a planned removal.
// Kubernetes readiness and cached topology roles alone cannot establish that
// a previous cohort change or leader election has finished. Missing evidence
// blocks removal; this function never changes Multigres consensus state.
func CheckDisruption(
	ctx context.Context,
	store topoclient.Store,
	rpc rpcclient.MultipoolerClient,
	shard *multigresv1alpha1.Shard,
	availablePodNames []string,
	targetName string,
) error {
	observed := map[string]*multipoolermanagerdatapb.StatusResponse{}
	var leader *clustermetadatapb.ID
	var target *clustermetadatapb.ID
	var rule *clustermetadatapb.ShardRule
	names := append(append([]string{}, availablePodNames...), targetName)
	for _, cell := range topo.CollectCells(shard) {
		poolers, err := store.GetMultipoolersByCell(ctx, cell, topo.ShardFilter(shard))
		if err != nil {
			return fmt.Errorf("observe cell %s: %w", cell, err)
		}
		for _, pooler := range poolers {
			name := matchPod(pooler, names)
			if name == "" || pooler.Id == nil {
				continue
			}
			if name == targetName {
				target = pooler.Id
			}
			rpcCtx, cancel := context.WithTimeout(ctx, statusRPCTimeout)
			resp, err := rpc.Status(
				rpcCtx,
				pooler.Multipooler,
				&multipoolermanagerdatapb.StatusRequest{},
			)
			cancel()
			if err != nil {
				if name == targetName {
					continue // An unhealthy extra must not block its own removal.
				}
				return fmt.Errorf("observe pooler %s: %w", name, err)
			}
			if !proto.Equal(resp.GetConsensusStatus().GetId(), pooler.Id) {
				if name == targetName && resp.GetConsensusStatus().GetId() == nil {
					continue // Missing target status is equivalent to an unreachable target.
				}
				return fmt.Errorf("pooler %s has no matching consensus identity", name)
			}
			key := topoclient.ClusterIDString(pooler.Id)
			if _, duplicate := observed[key]; duplicate {
				return fmt.Errorf("duplicate pooler identity %s", key)
			}
			observed[key] = resp
			if resp.GetStatus().
				GetPostgresStatus() ==
				multipoolermanagerdatapb.PostgresStatus_POSTGRES_STATUS_PRIMARY {
				if leader != nil || !topo.IsPrimaryPooler(pooler.Multipooler) {
					return fmt.Errorf("primary observations disagree")
				}
				leader = pooler.Id
				rule = resp.GetConsensusStatus().GetCurrentPosition().GetPosition().GetDecision()
			}
		}
	}
	if target == nil || leader == nil || rule.GetRuleNumber() == nil ||
		!proto.Equal(rule.GetLeaderId(), leader) {
		return fmt.Errorf("awaiting a registered target and a committed primary")
	}
	if proto.Equal(target, leader) && shard.Status.PodRoles[targetName] != "PRIMARY" {
		return fmt.Errorf("target became primary; refresh removal ordering")
	}
	if targetStatus := observed[topoclient.ClusterIDString(target)]; targetStatus != nil {
		position := targetStatus.GetConsensusStatus().GetCurrentPosition().GetPosition()
		comparison := consensus.CompareRulePosition(
			position,
			&clustermetadatapb.RulePosition{Decision: rule},
		)
		// An unhealthy extra may be behind the surviving quorum. A proposal,
		// newer decision, or conflicting decision at the same position is not
		// evidence of completed recovery and must not be ignored.
		conflicting := comparison == 0 && !proto.Equal(position.GetDecision(), rule)
		if position.GetProposal() != nil || comparison > 0 || conflicting {
			return fmt.Errorf("target has an unsettled consensus rule")
		}
	}
	primary := observed[topoclient.ClusterIDString(leader)]
	if _, readiness := poolerReadiness(primary, leader); !readiness.Ready {
		return fmt.Errorf("primary is not an eligible committed cohort member")
	}
	leadership := primary.GetAvailabilityStatus().GetLeadershipStatus()
	if leadership.GetSignal() != clustermetadatapb.LeadershipSignal_LEADERSHIP_SIGNAL_ACTIVE ||
		leadership.GetLeaderTerm() != rule.GetRuleNumber().GetCoordinatorTerm() ||
		!primary.GetStatus().GetPrimaryStatus().GetReady() {
		return fmt.Errorf("committed primary is not actively serving")
	}
	var remaining []*clustermetadatapb.ID
	for _, member := range rule.GetCohortMembers() {
		if proto.Equal(member, target) && !proto.Equal(member, leader) {
			continue
		}
		resp := observed[topoclient.ClusterIDString(member)]
		_, ready := poolerReadiness(resp, member)
		cs := resp.GetConsensusStatus()
		position := cs.GetCurrentPosition().GetPosition()
		settled := proto.Equal(position.GetDecision(), rule) && position.GetProposal() == nil
		eligible := !consensus.IsSelfRevoked(cs) && cs.GetRecruitBlockedUntil() == nil
		if !ready.Ready || !settled || !eligible {
			return fmt.Errorf(
				"cohort member %s has not recovered on the committed rule",
				topoclient.ClusterIDString(member),
			)
		}
		if !proto.Equal(member, target) {
			if !proto.Equal(member, leader) &&
				resp.GetStatus().
					GetPostgresStatus() !=
					multipoolermanagerdatapb.PostgresStatus_POSTGRES_STATUS_STANDBY {
				return fmt.Errorf(
					"surviving follower %s is not a standby",
					topoclient.ClusterIDString(member),
				)
			}
			remaining = append(remaining, member)
		}
	}
	policy, err := consensus.NewPolicyFromProto(rule.GetDurabilityPolicy())
	if err != nil {
		return fmt.Errorf("invalid committed durability policy: %w", err)
	}
	if err := policy.SatisfiedBy(remaining); err != nil {
		return fmt.Errorf("remaining cohort cannot satisfy durability: %w", err)
	}
	if err := consensus.CheckSufficientRecruitment(
		policy,
		rule.GetCohortMembers(),
		remaining,
	); err != nil {
		return fmt.Errorf("remaining cohort cannot recruit a leader: %w", err)
	}
	// Require the current primary to have streaming connections to the remaining
	// followers. A locally accepting standby can still be disconnected after a
	// failover, despite having replicated the same rule earlier.
	connected := map[string]bool{topoclient.ClusterIDString(leader): true}
	for _, follower := range primary.GetStatus().GetPrimaryStatus().GetConnectedFollowers() {
		connected[topoclient.ClusterIDString(follower)] = true
	}
	for _, member := range remaining {
		if !connected[topoclient.ClusterIDString(member)] {
			return fmt.Errorf(
				"cohort member %s is not connected to the primary",
				topoclient.ClusterIDString(member),
			)
		}
	}
	return ctx.Err()
}
