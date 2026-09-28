package store_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/arodriguezp2003/ion-mem/internal/store"
)

func mustObservationOfType(t *testing.T, s *store.Store, sessionID, typ string) store.Observation {
	t.Helper()
	obs, err := s.AddObservation(context.Background(), store.AddObservationParams{
		SessionID: sessionID,
		Type:      typ,
		Title:     "title-" + typ + "-" + randomSuffix(),
		Content:   "content " + randomSuffix(),
		Project:   "test-project",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("mustObservationOfType: %v", err)
	}
	return obs
}

func TestSetObservationStatus_Superseded(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "test-project")

	oldObs := mustObservation(t, s, sess.ID)
	newObs := mustObservation(t, s, sess.ID)

	if err := s.SetObservationStatus(ctx, oldObs.ID, store.StatusSuperseded, &newObs.ID, "replaced by newer approach"); err != nil {
		t.Fatalf("SetObservationStatus: %v", err)
	}

	got, err := s.GetObservation(ctx, oldObs.ID)
	if err != nil {
		t.Fatalf("GetObservation: %v", err)
	}
	if got.Status != store.StatusSuperseded {
		t.Errorf("Status = %q, want %q", got.Status, store.StatusSuperseded)
	}
	if got.SupersededBy == nil || *got.SupersededBy != newObs.ID {
		t.Errorf("SupersededBy = %v, want %d", got.SupersededBy, newObs.ID)
	}
	if got.StatusReason == nil || *got.StatusReason != "replaced by newer approach" {
		t.Errorf("StatusReason = %v, want %q", got.StatusReason, "replaced by newer approach")
	}
	if got.StatusChangedAt == nil || *got.StatusChangedAt == "" {
		t.Errorf("StatusChangedAt = %v, want non-empty", got.StatusChangedAt)
	}
}

func TestSetObservationStatus_BackToActiveClearsSupersession(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "test-project")

	oldObs := mustObservation(t, s, sess.ID)
	newObs := mustObservation(t, s, sess.ID)

	if err := s.SetObservationStatus(ctx, oldObs.ID, store.StatusSuperseded, &newObs.ID, "reason"); err != nil {
		t.Fatalf("SetObservationStatus superseded: %v", err)
	}
	if err := s.SetObservationStatus(ctx, oldObs.ID, store.StatusActive, nil, ""); err != nil {
		t.Fatalf("SetObservationStatus active: %v", err)
	}

	got, err := s.GetObservation(ctx, oldObs.ID)
	if err != nil {
		t.Fatalf("GetObservation: %v", err)
	}
	if got.Status != store.StatusActive {
		t.Errorf("Status = %q, want %q", got.Status, store.StatusActive)
	}
	if got.SupersededBy != nil {
		t.Errorf("SupersededBy = %v, want nil", got.SupersededBy)
	}
	if got.StatusReason != nil {
		t.Errorf("StatusReason = %v, want nil", got.StatusReason)
	}
}

func TestSetObservationStatus_RejectsInvalidStatus(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "test-project")
	obs := mustObservation(t, s, sess.ID)

	err := s.SetObservationStatus(ctx, obs.ID, "archived", nil, "")
	if err == nil {
		t.Fatal("expected error for invalid status, got nil")
	}
}

func TestSetObservationStatus_SupersededRequiresPointer(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "test-project")
	obs := mustObservation(t, s, sess.ID)

	err := s.SetObservationStatus(ctx, obs.ID, store.StatusSuperseded, nil, "")
	if err == nil {
		t.Fatal("expected error when superseded_by is nil, got nil")
	}
}

func TestSetObservationStatus_RejectsSelfSupersede(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "test-project")
	obs := mustObservation(t, s, sess.ID)

	err := s.SetObservationStatus(ctx, obs.ID, store.StatusSuperseded, &obs.ID, "")
	if err == nil {
		t.Fatal("expected error when an observation supersedes itself, got nil")
	}
}

// ─── cycle detection ──────────────────────────────────────────────────────────

func TestSetObservationStatus_RejectsTwoNodeCycle(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "test-project")
	obs1 := mustObservation(t, s, sess.ID)
	obs2 := mustObservation(t, s, sess.ID)

	// obs1 superseded_by obs2.
	if err := s.SetObservationStatus(ctx, obs1.ID, store.StatusSuperseded, &obs2.ID, ""); err != nil {
		t.Fatalf("SetObservationStatus obs1->obs2: %v", err)
	}

	// obs2 superseded_by obs1 would close the loop: obs1 -> obs2 -> obs1.
	err := s.SetObservationStatus(ctx, obs2.ID, store.StatusSuperseded, &obs1.ID, "")
	if err == nil {
		t.Fatal("expected error for a 2-node superseded_by cycle, got nil")
	}
}

func TestSetObservationStatus_RejectsThreeNodeCycle(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "test-project")
	obs1 := mustObservation(t, s, sess.ID)
	obs2 := mustObservation(t, s, sess.ID)
	obs3 := mustObservation(t, s, sess.ID)

	// obs1 -> obs2 -> obs3.
	if err := s.SetObservationStatus(ctx, obs1.ID, store.StatusSuperseded, &obs2.ID, ""); err != nil {
		t.Fatalf("SetObservationStatus obs1->obs2: %v", err)
	}
	if err := s.SetObservationStatus(ctx, obs2.ID, store.StatusSuperseded, &obs3.ID, ""); err != nil {
		t.Fatalf("SetObservationStatus obs2->obs3: %v", err)
	}

	// obs3 -> obs1 would close the loop: obs1 -> obs2 -> obs3 -> obs1.
	err := s.SetObservationStatus(ctx, obs3.ID, store.StatusSuperseded, &obs1.ID, "")
	if err == nil {
		t.Fatal("expected error for a 3-node superseded_by cycle, got nil")
	}
}

func TestSetObservationStatus_RejectsCrossProjectSupersede(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sessA := mustSession(t, s, "project-a")
	sessB := mustSession(t, s, "project-b")

	obsA := mustObservationForProject(t, s, sessA.ID, "project-a")
	obsB := mustObservationForProject(t, s, sessB.ID, "project-b")

	err := s.SetObservationStatus(ctx, obsA.ID, store.StatusSuperseded, &obsB.ID, "")
	if err == nil {
		t.Fatal("expected error for cross-project supersede, got nil")
	}
}

func TestSetObservationStatus_RejectsUnknownSupersededBy(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "test-project")
	obs := mustObservation(t, s, sess.ID)

	err := s.SetObservationStatus(ctx, obs.ID, store.StatusSuperseded, int64Ptr(999999), "")
	if err == nil {
		t.Fatal("expected error for unknown superseded_by id, got nil")
	}
}

// TestSetObservationStatus_RejectsSupersededByNonActiveTarget verifies that
// superseded_by must always point at the newest active row: pointing at a
// row that is itself already superseded (a dead pointer / partial chain) is
// rejected with a clear error naming the target's actual status.
func TestSetObservationStatus_RejectsSupersededByNonActiveTarget(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "test-project")

	obs1 := mustObservation(t, s, sess.ID)
	obs2 := mustObservation(t, s, sess.ID)
	obs3 := mustObservation(t, s, sess.ID)

	// obs1 -> obs2 (obs2 is active, so this is fine).
	if err := s.SetObservationStatus(ctx, obs1.ID, store.StatusSuperseded, &obs2.ID, ""); err != nil {
		t.Fatalf("SetObservationStatus obs1->obs2: %v", err)
	}

	// obs3 -> obs1 must be rejected: obs1 is now superseded, not active.
	err := s.SetObservationStatus(ctx, obs3.ID, store.StatusSuperseded, &obs1.ID, "")
	if err == nil {
		t.Fatal("expected error superseding via a non-active target, got nil")
	}
	wantMsg := fmt.Sprintf("superseded_by target #%d is %s; point at the newest active row", obs1.ID, store.StatusSuperseded)
	if !strings.Contains(err.Error(), wantMsg) {
		t.Errorf("error = %q, want it to contain %q", err.Error(), wantMsg)
	}
}

// TestSetObservationStatus_RejectsSupersededByObsoleteTarget verifies the
// same rule for an obsolete target, not just a superseded one.
func TestSetObservationStatus_RejectsSupersededByObsoleteTarget(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "test-project")

	obsolete := mustObservation(t, s, sess.ID)
	newer := mustObservation(t, s, sess.ID)
	if err := s.SetObservationStatus(ctx, obsolete.ID, store.StatusObsolete, nil, ""); err != nil {
		t.Fatalf("SetObservationStatus obsolete: %v", err)
	}

	err := s.SetObservationStatus(ctx, newer.ID, store.StatusSuperseded, &obsolete.ID, "")
	if err == nil {
		t.Fatal("expected error superseding via an obsolete target, got nil")
	}
	wantMsg := fmt.Sprintf("superseded_by target #%d is %s; point at the newest active row", obsolete.ID, store.StatusObsolete)
	if !strings.Contains(err.Error(), wantMsg) {
		t.Errorf("error = %q, want it to contain %q", err.Error(), wantMsg)
	}
}

func TestSetObservationStatus_NotFound(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	err := s.SetObservationStatus(ctx, 999999, store.StatusActive, nil, "")
	if !errors.Is(err, store.ErrObservationNotFound) {
		t.Fatalf("SetObservationStatus for missing id: got %v, want ErrObservationNotFound", err)
	}
}

// ─── permanence rules (bugfix / discovery) ───────────────────────────────────

func TestSetObservationStatus_RefusesObsoleteForPermanentType(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "test-project")
	obs := mustObservationOfType(t, s, sess.ID, "bugfix")

	err := s.SetObservationStatus(ctx, obs.ID, store.StatusObsolete, nil, "")
	if err == nil {
		t.Fatal("expected error making a bugfix observation obsolete, got nil")
	}
}

func TestSetObservationStatus_AllowsSupersededForPermanentTypeWhenTargetPermanent(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "test-project")
	oldFix := mustObservationOfType(t, s, sess.ID, "bugfix")
	newFix := mustObservationOfType(t, s, sess.ID, "bugfix")

	if err := s.SetObservationStatus(ctx, oldFix.ID, store.StatusSuperseded, &newFix.ID, "root cause fixed properly"); err != nil {
		t.Fatalf("SetObservationStatus: %v", err)
	}
}

func TestSetObservationStatus_RefusesSupersededForPermanentTypeWhenTargetNotPermanent(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "test-project")
	fix := mustObservationOfType(t, s, sess.ID, "bugfix")
	decision := mustObservationOfType(t, s, sess.ID, "decision")

	err := s.SetObservationStatus(ctx, fix.ID, store.StatusSuperseded, &decision.ID, "")
	if err == nil {
		t.Fatal("expected error superseding a bugfix with a non-permanent type, got nil")
	}
}

func TestSetObservationStatus_RefusesObsoleteForDiscovery(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "test-project")
	obs := mustObservationOfType(t, s, sess.ID, "discovery")

	err := s.SetObservationStatus(ctx, obs.ID, store.StatusObsolete, nil, "")
	if err == nil {
		t.Fatal("expected error making a discovery observation obsolete, got nil")
	}
}

// ─── ListObservationsByStatus ────────────────────────────────────────────────

func TestListObservationsByStatus_FiltersByProjectAndStatus(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "test-project")

	active := mustObservation(t, s, sess.ID)
	superseded := mustObservation(t, s, sess.ID)
	if err := s.SetObservationStatus(ctx, superseded.ID, store.StatusSuperseded, &active.ID, ""); err != nil {
		t.Fatalf("SetObservationStatus: %v", err)
	}

	got, err := s.ListObservationsByStatus(ctx, "test-project", store.StatusSuperseded, 10)
	if err != nil {
		t.Fatalf("ListObservationsByStatus: %v", err)
	}
	if len(got) != 1 || got[0].ID != superseded.ID {
		t.Fatalf("ListObservationsByStatus = %+v, want exactly [%d]", got, superseded.ID)
	}

	gotActive, err := s.ListObservationsByStatus(ctx, "test-project", store.StatusActive, 10)
	if err != nil {
		t.Fatalf("ListObservationsByStatus active: %v", err)
	}
	found := false
	for _, o := range gotActive {
		if o.ID == active.ID {
			found = true
		}
		if o.ID == superseded.ID {
			t.Errorf("superseded observation %d leaked into active list", superseded.ID)
		}
	}
	if !found {
		t.Errorf("active observation %d missing from active list", active.ID)
	}
}

func int64Ptr(n int64) *int64 { return &n }
