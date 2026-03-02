package backuphealth_test

import (
	"testing"
	"time"

	"github.com/multigres/multigres/go/pb/multipoolermanagerdata"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	multigresv1alpha1 "github.com/numtide/multigres-operator/api/v1alpha1"
	"github.com/numtide/multigres-operator/pkg/data-handler/backuphealth"
)

func TestEvaluateBackups_Healthy(t *testing.T) {
	t.Parallel()

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
			Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
		},
	}

	recentID := time.Now().Add(-1 * time.Hour).Format("20060102-150405")
	backups := []*multipoolermanagerdata.BackupMetadata{
		{
			BackupId: recentID,
			Status:   multipoolermanagerdata.BackupMetadata_COMPLETE,
			Type:     "full",
		},
	}

	result := backuphealth.EvaluateBackups(shard, backups)
	if !result.Healthy {
		t.Errorf("expected healthy, got message: %s", result.Message)
	}
	if result.LastBackupType != "full" {
		t.Errorf("expected type=full, got %s", result.LastBackupType)
	}
	if result.LastBackupTime == nil {
		t.Error("expected LastBackupTime to be set")
	}
}

func TestEvaluateBackups_Stale(t *testing.T) {
	t.Parallel()

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
			Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
		},
	}

	oldID := time.Now().Add(-48 * time.Hour).Format("20060102-150405")
	backups := []*multipoolermanagerdata.BackupMetadata{
		{
			BackupId: oldID,
			Status:   multipoolermanagerdata.BackupMetadata_COMPLETE,
			Type:     "diff",
		},
	}

	result := backuphealth.EvaluateBackups(shard, backups)
	if result.Healthy {
		t.Error("expected unhealthy for 48h-old backup")
	}
	if result.LastBackupType != "diff" {
		t.Errorf("expected type=diff, got %s", result.LastBackupType)
	}
}

func TestEvaluateBackups_NoBackups(t *testing.T) {
	t.Parallel()

	shard := &multigresv1alpha1.Shard{}
	result := backuphealth.EvaluateBackups(shard, nil)
	if result.Healthy {
		t.Error("expected unhealthy when no backups")
	}
	if result.Message != "No backups found" {
		t.Errorf("unexpected message: %s", result.Message)
	}
}

func TestEvaluateBackups_NoCompleted(t *testing.T) {
	t.Parallel()

	shard := &multigresv1alpha1.Shard{}
	backups := []*multipoolermanagerdata.BackupMetadata{
		{
			BackupId: "20260224-120000",
			Status:   multipoolermanagerdata.BackupMetadata_INCOMPLETE,
			Type:     "full",
		},
	}

	result := backuphealth.EvaluateBackups(shard, backups)
	if result.Healthy {
		t.Error("expected unhealthy when no completed backups")
	}
	if result.Message != "No completed backups found" {
		t.Errorf("unexpected message: %s", result.Message)
	}
}

func TestEvaluateBackups_SelectsMostRecent(t *testing.T) {
	t.Parallel()

	shard := &multigresv1alpha1.Shard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-shard",
			Namespace: "default",
			Labels:    map[string]string{"multigres.com/cluster": "test-cluster"},
		},
	}

	recentID := time.Now().Add(-1 * time.Hour).Format("20060102-150405")
	olderID := time.Now().Add(-5 * time.Hour).Format("20060102-150405")
	backups := []*multipoolermanagerdata.BackupMetadata{
		{
			BackupId: olderID,
			Status:   multipoolermanagerdata.BackupMetadata_COMPLETE,
			Type:     "full",
		},
		{
			BackupId: recentID,
			Status:   multipoolermanagerdata.BackupMetadata_COMPLETE,
			Type:     "incr",
		},
	}

	result := backuphealth.EvaluateBackups(shard, backups)
	if result.LastBackupType != "incr" {
		t.Errorf("expected most recent backup type=incr, got %s", result.LastBackupType)
	}
}

func TestApplyBackupHealth(t *testing.T) {
	t.Parallel()

	t.Run("sets healthy condition", func(t *testing.T) {
		t.Parallel()

		shard := &multigresv1alpha1.Shard{
			ObjectMeta: metav1.ObjectMeta{Generation: 5},
		}
		now := metav1.Now()
		result := &backuphealth.Result{
			Healthy:        true,
			LastBackupTime: &now,
			LastBackupType: "full",
			Message:        "backup is healthy",
		}

		backuphealth.ApplyBackupHealth(shard, result)

		if len(shard.Status.Conditions) != 1 {
			t.Fatalf("expected 1 condition, got %d", len(shard.Status.Conditions))
		}
		c := shard.Status.Conditions[0]
		if c.Type != backuphealth.ConditionBackupHealthy {
			t.Errorf(
				"expected condition type %s, got %s",
				backuphealth.ConditionBackupHealthy,
				c.Type,
			)
		}
		if c.Status != metav1.ConditionTrue {
			t.Errorf("expected True, got %s", c.Status)
		}
		if c.Reason != "BackupRecent" {
			t.Errorf("expected reason BackupRecent, got %s", c.Reason)
		}
		if shard.Status.LastBackupType != "full" {
			t.Errorf("expected LastBackupType=full, got %s", shard.Status.LastBackupType)
		}
	})

	t.Run("sets unhealthy condition", func(t *testing.T) {
		t.Parallel()

		shard := &multigresv1alpha1.Shard{}
		result := &backuphealth.Result{
			Healthy: false,
			Message: "backup is stale",
		}

		backuphealth.ApplyBackupHealth(shard, result)

		if len(shard.Status.Conditions) != 1 {
			t.Fatalf("expected 1 condition, got %d", len(shard.Status.Conditions))
		}
		c := shard.Status.Conditions[0]
		if c.Status != metav1.ConditionFalse {
			t.Errorf("expected False, got %s", c.Status)
		}
		if c.Reason != "BackupStale" {
			t.Errorf("expected reason BackupStale, got %s", c.Reason)
		}
	})

	t.Run("nil result is no-op", func(t *testing.T) {
		t.Parallel()

		shard := &multigresv1alpha1.Shard{}
		backuphealth.ApplyBackupHealth(shard, nil)
		if len(shard.Status.Conditions) != 0 {
			t.Error("expected no conditions for nil result")
		}
	})
}

func TestParseBackupTime(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input string
		want  time.Time
	}{
		"valid": {
			input: "20260224-143055",
			want:  time.Date(2026, 2, 24, 14, 30, 55, 0, time.UTC),
		},
		"with suffix": {
			input: "20260224-143055F123456",
			want:  time.Date(2026, 2, 24, 14, 30, 55, 0, time.UTC),
		},
		"too short": {input: "20260224", want: time.Time{}},
		"empty":     {input: "", want: time.Time{}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := backuphealth.ParseBackupTime(tc.input)
			if !got.Equal(tc.want) {
				t.Errorf("ParseBackupTime(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestParseBackupTime_InvalidFormat(t *testing.T) {
	t.Parallel()
	got := backuphealth.ParseBackupTime("ABCDEFG-HIJKLMN")
	if !got.IsZero() {
		t.Errorf("expected zero time for invalid format, got %v", got)
	}
}
