package store_test

import (
	"context"
	"strings"
	"testing"

	"github.com/arodriguezp2003/ion-mem/internal/bundle"
	"github.com/arodriguezp2003/ion-mem/internal/store"
)

// ─── ExportProject ────────────────────────────────────────────────────────────

func TestExportProject_ScopesToProjectAndExcludesDeletedByDefault(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	sessA := mustSession(t, s, "proj-a")
	sessB := mustSession(t, s, "proj-b")

	obsA := mustObservationForProject(t, s, sessA.ID, "proj-a")
	mustObservationForProject(t, s, sessB.ID, "proj-b")

	deletedObs := mustObservationForProject(t, s, sessA.ID, "proj-a")
	if err := s.DeleteObservation(ctx, deletedObs.ID, false); err != nil {
		t.Fatalf("DeleteObservation: %v", err)
	}

	b, err := s.ExportProject(ctx, "proj-a", store.ExportProjectOptions{})
	if err != nil {
		t.Fatalf("ExportProject: %v", err)
	}

	if len(b.Observations) != 1 {
		t.Fatalf("observations = %d, want 1 (proj-b and soft-deleted excluded)", len(b.Observations))
	}
	if b.Observations[0].SyncID != obsA.SyncID {
		t.Errorf("sync_id = %q, want %q", b.Observations[0].SyncID, obsA.SyncID)
	}
	if b.Manifest.Project != "proj-a" {
		t.Errorf("manifest.project = %q, want proj-a", b.Manifest.Project)
	}
	if b.Manifest.FormatVersion == 0 {
		t.Error("manifest.format_version must be set")
	}
	if b.Manifest.IncludesPrompts {
		t.Error("IncludesPrompts must be false by default")
	}
}

func TestExportProject_IncludeDeleted(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	sess := mustSession(t, s, "proj-a")
	deletedObs := mustObservationForProject(t, s, sess.ID, "proj-a")
	if err := s.DeleteObservation(ctx, deletedObs.ID, false); err != nil {
		t.Fatalf("DeleteObservation: %v", err)
	}

	b, err := s.ExportProject(ctx, "proj-a", store.ExportProjectOptions{IncludeDeleted: true})
	if err != nil {
		t.Fatalf("ExportProject: %v", err)
	}
	if len(b.Observations) != 1 {
		t.Fatalf("observations = %d, want 1", len(b.Observations))
	}
}

func TestExportProject_ResolvesSupersededBySyncID(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "proj-a")

	oldObs := mustObservationForProject(t, s, sess.ID, "proj-a")
	newObs := mustObservationForProject(t, s, sess.ID, "proj-a")

	if err := s.SetObservationStatus(ctx, oldObs.ID, store.StatusSuperseded, &newObs.ID, "replaced"); err != nil {
		t.Fatalf("SetObservationStatus: %v", err)
	}

	b, err := s.ExportProject(ctx, "proj-a", store.ExportProjectOptions{})
	if err != nil {
		t.Fatalf("ExportProject: %v", err)
	}

	var found *bundle.Observation
	for i := range b.Observations {
		if b.Observations[i].SyncID == oldObs.SyncID {
			found = &b.Observations[i]
		}
	}
	if found == nil {
		t.Fatal("old observation not found in export")
	}
	if found.SupersededBySyncID == nil || *found.SupersededBySyncID != newObs.SyncID {
		t.Errorf("superseded_by_sync_id = %v, want %q", found.SupersededBySyncID, newObs.SyncID)
	}
}

func TestExportProject_IncludePrompts(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "proj-a")
	mustPrompt(t, s, sess.ID, "hello", "proj-a")

	b, err := s.ExportProject(ctx, "proj-a", store.ExportProjectOptions{IncludePrompts: true})
	if err != nil {
		t.Fatalf("ExportProject: %v", err)
	}
	if !b.Manifest.IncludesPrompts {
		t.Error("IncludesPrompts must be true")
	}
	if len(b.Prompts) != 1 {
		t.Fatalf("prompts = %d, want 1", len(b.Prompts))
	}
	if b.Prompts[0].Content != "hello" {
		t.Errorf("prompt content = %q, want hello", b.Prompts[0].Content)
	}

	// Without the option, prompts must be empty.
	b2, err := s.ExportProject(ctx, "proj-a", store.ExportProjectOptions{})
	if err != nil {
		t.Fatalf("ExportProject: %v", err)
	}
	if len(b2.Prompts) != 0 {
		t.Errorf("prompts = %d, want 0 when IncludePrompts is false", len(b2.Prompts))
	}
}

func TestExportProject_IncludesRevisions(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	sess := mustSession(t, s, "proj-a")

	obs := mustObservationForProject(t, s, sess.ID, "proj-a")
	newTitle := "updated title"
	newContent := "updated content"
	if _, err := s.UpdateObservation(ctx, obs.ID, store.UpdateObservationParams{Title: &newTitle, Content: &newContent}); err != nil {
		t.Fatalf("UpdateObservation: %v", err)
	}

	b, err := s.ExportProject(ctx, "proj-a", store.ExportProjectOptions{})
	if err != nil {
		t.Fatalf("ExportProject: %v", err)
	}
	if len(b.Revisions) != 1 {
		t.Fatalf("revisions = %d, want 1", len(b.Revisions))
	}
	if b.Revisions[0].ObservationSyncID != obs.SyncID {
		t.Errorf("revision.observation_sync_id = %q, want %q", b.Revisions[0].ObservationSyncID, obs.SyncID)
	}
}

// ─── ImportProject ────────────────────────────────────────────────────────────

func TestImportProject_InsertsNewObservations(t *testing.T) {
	src := mustOpen(t)
	dst := mustOpen(t)
	ctx := context.Background()

	sess := mustSession(t, src, "shared-proj")
	mustObservationForProject(t, src, sess.ID, "shared-proj")

	b, err := src.ExportProject(ctx, "shared-proj", store.ExportProjectOptions{})
	if err != nil {
		t.Fatalf("ExportProject: %v", err)
	}

	report, err := dst.ImportProject(ctx, b, store.ImportOptions{TargetProject: "shared-proj"})
	if err != nil {
		t.Fatalf("ImportProject: %v", err)
	}
	if report.Inserted != 1 {
		t.Errorf("Inserted = %d, want 1", report.Inserted)
	}
	if report.SessionID == "" {
		t.Error("SessionID must be set on the report")
	}

	obs, err := dst.RecentObservations(ctx, store.RecentObservationsParams{Project: "shared-proj"})
	if err != nil {
		t.Fatalf("RecentObservations: %v", err)
	}
	if len(obs) != 1 {
		t.Fatalf("dst observations = %d, want 1", len(obs))
	}
	if obs[0].SyncID != b.Observations[0].SyncID {
		t.Errorf("sync_id = %q, want %q", obs[0].SyncID, b.Observations[0].SyncID)
	}
}

func TestImportProject_CarriesOverRevisionsForNewlyInsertedOnly(t *testing.T) {
	src := mustOpen(t)
	dst := mustOpen(t)
	ctx := context.Background()

	sess := mustSession(t, src, "shared-proj")
	obs := mustObservationForProject(t, src, sess.ID, "shared-proj")
	newTitle := "v2 title"
	if _, err := src.UpdateObservation(ctx, obs.ID, store.UpdateObservationParams{Title: &newTitle}); err != nil {
		t.Fatalf("UpdateObservation: %v", err)
	}

	b, err := src.ExportProject(ctx, "shared-proj", store.ExportProjectOptions{})
	if err != nil {
		t.Fatalf("ExportProject: %v", err)
	}
	if len(b.Revisions) != 1 {
		t.Fatalf("expected 1 revision in export, got %d", len(b.Revisions))
	}

	if _, err := dst.ImportProject(ctx, b, store.ImportOptions{TargetProject: "shared-proj"}); err != nil {
		t.Fatalf("ImportProject: %v", err)
	}

	dstObs, err := dst.RecentObservations(ctx, store.RecentObservationsParams{Project: "shared-proj"})
	if err != nil || len(dstObs) != 1 {
		t.Fatalf("RecentObservations: %v (len=%d)", err, len(dstObs))
	}
	revs, err := dst.ListRevisions(ctx, dstObs[0].ID)
	if err != nil {
		t.Fatalf("ListRevisions: %v", err)
	}
	if len(revs) != 1 {
		t.Fatalf("revisions = %d, want 1", len(revs))
	}
}

func TestImportProject_SkipsIdenticalExisting(t *testing.T) {
	src := mustOpen(t)
	dst := mustOpen(t)
	ctx := context.Background()

	sess := mustSession(t, src, "shared-proj")
	mustObservationForProject(t, src, sess.ID, "shared-proj")
	b, err := src.ExportProject(ctx, "shared-proj", store.ExportProjectOptions{})
	if err != nil {
		t.Fatalf("ExportProject: %v", err)
	}

	if _, err := dst.ImportProject(ctx, b, store.ImportOptions{TargetProject: "shared-proj"}); err != nil {
		t.Fatalf("first ImportProject: %v", err)
	}

	// Import the same bundle again — every row should now be skipped.
	report, err := dst.ImportProject(ctx, b, store.ImportOptions{TargetProject: "shared-proj"})
	if err != nil {
		t.Fatalf("second ImportProject: %v", err)
	}
	if report.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", report.Skipped)
	}
	if report.Inserted != 0 {
		t.Errorf("Inserted = %d, want 0", report.Inserted)
	}
}

func TestImportProject_KeepsLocalOnConflictByDefault(t *testing.T) {
	src := mustOpen(t)
	dst := mustOpen(t)
	ctx := context.Background()

	sessSrc := mustSession(t, src, "shared-proj")
	obsSrc := mustObservationForProject(t, src, sessSrc.ID, "shared-proj")
	b, err := src.ExportProject(ctx, "shared-proj", store.ExportProjectOptions{})
	if err != nil {
		t.Fatalf("ExportProject: %v", err)
	}

	if _, err := dst.ImportProject(ctx, b, store.ImportOptions{TargetProject: "shared-proj"}); err != nil {
		t.Fatalf("first ImportProject: %v", err)
	}

	// Diverge the bundle's copy (simulate teammate editing their own row after export).
	changedTitle := "diverged title"
	b.Observations[0].Title = changedTitle
	b.Observations[0].NormalizedHash = "different-hash"

	report, err := dst.ImportProject(ctx, b, store.ImportOptions{TargetProject: "shared-proj"})
	if err != nil {
		t.Fatalf("second ImportProject: %v", err)
	}
	if len(report.Conflicts) != 1 {
		t.Fatalf("Conflicts = %d, want 1", len(report.Conflicts))
	}
	if report.Conflicts[0].SyncID != obsSrc.SyncID {
		t.Errorf("conflict sync_id = %q, want %q", report.Conflicts[0].SyncID, obsSrc.SyncID)
	}

	// Local row must be unchanged.
	dstObs, err := dst.GetObservation(ctx, mustFindByTitle(t, dst, ctx, obsSrc.Title))
	if err != nil {
		t.Fatalf("GetObservation: %v", err)
	}
	if dstObs.Title == changedTitle {
		t.Error("local row must not be overwritten by default (PreferBundle=false)")
	}
}

func TestImportProject_PreferBundleUpdatesAndRecordsRevision(t *testing.T) {
	src := mustOpen(t)
	dst := mustOpen(t)
	ctx := context.Background()

	sessSrc := mustSession(t, src, "shared-proj")
	obsSrc := mustObservationForProject(t, src, sessSrc.ID, "shared-proj")
	b, err := src.ExportProject(ctx, "shared-proj", store.ExportProjectOptions{})
	if err != nil {
		t.Fatalf("ExportProject: %v", err)
	}

	if _, err := dst.ImportProject(ctx, b, store.ImportOptions{TargetProject: "shared-proj"}); err != nil {
		t.Fatalf("first ImportProject: %v", err)
	}

	changedTitle := "bundle wins title"
	b.Observations[0].Title = changedTitle
	b.Observations[0].Content = "bundle wins content"
	b.Observations[0].NormalizedHash = "bundle-hash"

	report, err := dst.ImportProject(ctx, b, store.ImportOptions{TargetProject: "shared-proj", PreferBundle: true})
	if err != nil {
		t.Fatalf("second ImportProject: %v", err)
	}
	if report.Updated != 1 {
		t.Errorf("Updated = %d, want 1", report.Updated)
	}

	localID := mustFindByTitle(t, dst, ctx, changedTitle)
	dstObs, err := dst.GetObservation(ctx, localID)
	if err != nil {
		t.Fatalf("GetObservation: %v", err)
	}
	if dstObs.Title != changedTitle {
		t.Errorf("title = %q, want %q (PreferBundle must win)", dstObs.Title, changedTitle)
	}

	revs, err := dst.ListRevisions(ctx, dstObs.ID)
	if err != nil {
		t.Fatalf("ListRevisions: %v", err)
	}
	if len(revs) != 1 {
		t.Fatalf("revisions = %d, want 1 (before-image captured on PreferBundle update)", len(revs))
	}
	if revs[0].Title != obsSrc.Title {
		t.Errorf("revision title = %q, want original %q", revs[0].Title, obsSrc.Title)
	}
}

func TestImportProject_PreferBundleSyncsToolNameScopeLastSeenAtButKeepsLocalSessionAndDuplicateCount(t *testing.T) {
	src := mustOpen(t)
	dst := mustOpen(t)
	ctx := context.Background()

	sessSrc := mustSession(t, src, "shared-proj")
	obsSrc, err := src.AddObservation(ctx, store.AddObservationParams{
		SessionID: sessSrc.ID,
		Type:      "decision",
		Title:     "original title",
		Content:   "original content",
		ToolName:  "original-tool",
		Project:   "shared-proj",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	b, err := src.ExportProject(ctx, "shared-proj", store.ExportProjectOptions{})
	if err != nil {
		t.Fatalf("ExportProject: %v", err)
	}

	if _, err := dst.ImportProject(ctx, b, store.ImportOptions{TargetProject: "shared-proj"}); err != nil {
		t.Fatalf("first ImportProject: %v", err)
	}

	localID := mustFindBySyncID(t, dst, ctx, obsSrc.SyncID)

	// Simulate independent local activity: bump duplicate_count and re-point
	// session_id at a different (but real, FK-valid) local session, so we
	// can prove PreferBundle does not clobber either.
	localOnlySession := mustSession(t, dst, "shared-proj")
	if _, err := dst.DB().ExecContext(ctx,
		`UPDATE observations SET duplicate_count=5, session_id=? WHERE id=?`,
		localOnlySession.ID, localID,
	); err != nil {
		t.Fatalf("simulate local drift: %v", err)
	}
	localSessionIDBefore := localOnlySession.ID

	// Diverge the bundle's copy and change tool_name/scope/last_seen_at.
	changedTitle := "bundle wins title 2"
	b.Observations[0].Title = changedTitle
	b.Observations[0].NormalizedHash = "bundle-hash-2"
	b.Observations[0].ToolName = strPtrLocal("bundle-tool")
	b.Observations[0].Scope = "personal"
	b.Observations[0].LastSeenAt = "2026-06-01T00:00:00Z"

	report, err := dst.ImportProject(ctx, b, store.ImportOptions{TargetProject: "shared-proj", PreferBundle: true})
	if err != nil {
		t.Fatalf("second ImportProject: %v", err)
	}
	if report.Updated != 1 {
		t.Fatalf("Updated = %d, want 1", report.Updated)
	}

	updated, err := dst.GetObservation(ctx, localID)
	if err != nil {
		t.Fatalf("GetObservation: %v", err)
	}
	if updated.ToolName == nil || *updated.ToolName != "bundle-tool" {
		t.Errorf("tool_name = %v, want bundle-tool", updated.ToolName)
	}
	if updated.Scope != "personal" {
		t.Errorf("scope = %q, want personal", updated.Scope)
	}
	if updated.LastSeenAt != "2026-06-01T00:00:00Z" {
		t.Errorf("last_seen_at = %q, want 2026-06-01T00:00:00Z", updated.LastSeenAt)
	}
	if updated.SessionID != localSessionIDBefore {
		t.Errorf("session_id = %q, want local value %q preserved (never overwritten by bundle)", updated.SessionID, localSessionIDBefore)
	}
	if updated.DuplicateCount != 5 {
		t.Errorf("duplicate_count = %d, want 5 (local value preserved, never overwritten by bundle)", updated.DuplicateCount)
	}
}

func strPtrLocal(s string) *string { return &s }

func TestImportProject_RemapsSupersededBy(t *testing.T) {
	src := mustOpen(t)
	dst := mustOpen(t)
	ctx := context.Background()

	sess := mustSession(t, src, "shared-proj")
	oldObs := mustObservationForProject(t, src, sess.ID, "shared-proj")
	newObs := mustObservationForProject(t, src, sess.ID, "shared-proj")
	if err := src.SetObservationStatus(ctx, oldObs.ID, store.StatusSuperseded, &newObs.ID, "replaced"); err != nil {
		t.Fatalf("SetObservationStatus: %v", err)
	}

	b, err := src.ExportProject(ctx, "shared-proj", store.ExportProjectOptions{})
	if err != nil {
		t.Fatalf("ExportProject: %v", err)
	}

	if _, err := dst.ImportProject(ctx, b, store.ImportOptions{TargetProject: "shared-proj"}); err != nil {
		t.Fatalf("ImportProject: %v", err)
	}

	dstOldID := mustFindBySyncID(t, dst, ctx, oldObs.SyncID)
	dstNewID := mustFindBySyncID(t, dst, ctx, newObs.SyncID)

	dstOld, err := dst.GetObservation(ctx, dstOldID)
	if err != nil {
		t.Fatalf("GetObservation old: %v", err)
	}
	if dstOld.Status != store.StatusSuperseded {
		t.Errorf("status = %q, want superseded", dstOld.Status)
	}
	if dstOld.SupersededBy == nil || *dstOld.SupersededBy != dstNewID {
		t.Errorf("superseded_by = %v, want local id %d", dstOld.SupersededBy, dstNewID)
	}
}

func TestImportProject_ObsoleteBugfixDowngradesToActive(t *testing.T) {
	dst := mustOpen(t)
	ctx := context.Background()

	b := bundle.Bundle{
		Manifest: bundle.Manifest{FormatVersion: bundle.FormatVersion, Project: "proj-a"},
		Observations: []bundle.Observation{
			{
				SyncID:         "obs-bugfix0000001",
				Type:           "bugfix",
				Title:          "fixed the thing",
				Content:        "root cause was X",
				Project:        "proj-a",
				Scope:          "project",
				NormalizedHash: "hash-bugfix",
				RevisionCount:  1,
				LastSeenAt:     "2026-01-01T00:00:00Z",
				CreatedAt:      "2026-01-01T00:00:00Z",
				UpdatedAt:      "2026-01-01T00:00:00Z",
				Status:         store.StatusObsolete,
			},
		},
	}

	report, err := dst.ImportProject(ctx, b, store.ImportOptions{TargetProject: "proj-a"})
	if err != nil {
		t.Fatalf("ImportProject: %v", err)
	}
	if report.Inserted != 1 {
		t.Fatalf("Inserted = %d, want 1", report.Inserted)
	}
	if len(report.StatusDowngrades) != 1 {
		t.Fatalf("StatusDowngrades = %d, want 1", len(report.StatusDowngrades))
	}
	dg := report.StatusDowngrades[0]
	if dg.SyncID != "obs-bugfix0000001" || dg.Wanted != store.StatusObsolete || dg.Reason == "" {
		t.Errorf("StatusDowngrade = %+v, want populated SyncID/Wanted/Reason", dg)
	}

	localID := mustFindBySyncID(t, dst, ctx, "obs-bugfix0000001")
	obs, err := dst.GetObservation(ctx, localID)
	if err != nil {
		t.Fatalf("GetObservation: %v", err)
	}
	if obs.Status != store.StatusActive {
		t.Errorf("status = %q, want active (permanent types can never be marked obsolete)", obs.Status)
	}
	if obs.StatusReason == nil || *obs.StatusReason == "" {
		t.Error("status_reason must explain the rejection")
	}
}

func TestImportProject_MutualSupersedeCycleRejected(t *testing.T) {
	dst := mustOpen(t)
	ctx := context.Background()

	b := bundle.Bundle{
		Manifest: bundle.Manifest{FormatVersion: bundle.FormatVersion, Project: "proj-a"},
		Observations: []bundle.Observation{
			{
				SyncID: "obs-mutualA000001", Type: "decision", Title: "A", Content: "content A",
				Project: "proj-a", Scope: "project", NormalizedHash: "hash-a", RevisionCount: 1,
				LastSeenAt: "2026-01-01T00:00:00Z", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
				Status: store.StatusSuperseded, SupersededBySyncID: strPtrLocal("obs-mutualB000001"),
			},
			{
				SyncID: "obs-mutualB000001", Type: "decision", Title: "B", Content: "content B",
				Project: "proj-a", Scope: "project", NormalizedHash: "hash-b", RevisionCount: 1,
				LastSeenAt: "2026-01-01T00:00:00Z", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
				Status: store.StatusSuperseded, SupersededBySyncID: strPtrLocal("obs-mutualA000001"),
			},
		},
	}

	report, err := dst.ImportProject(ctx, b, store.ImportOptions{TargetProject: "proj-a"})
	if err != nil {
		t.Fatalf("ImportProject: %v", err)
	}
	if report.Inserted != 2 {
		t.Fatalf("Inserted = %d, want 2", report.Inserted)
	}
	if len(report.StatusDowngrades) != 2 {
		t.Fatalf("StatusDowngrades = %d, want 2 (both sides of a mutual supersede must be rejected)", len(report.StatusDowngrades))
	}

	for _, syncID := range []string{"obs-mutualA000001", "obs-mutualB000001"} {
		localID := mustFindBySyncID(t, dst, ctx, syncID)
		obs, err := dst.GetObservation(ctx, localID)
		if err != nil {
			t.Fatalf("GetObservation(%s): %v", syncID, err)
		}
		if obs.Status != store.StatusActive {
			t.Errorf("%s status = %q, want active (cycle must reject both sides)", syncID, obs.Status)
		}
		if obs.SupersededBy != nil {
			t.Errorf("%s superseded_by = %v, want nil", syncID, obs.SupersededBy)
		}
		if obs.StatusReason == nil || *obs.StatusReason == "" {
			t.Errorf("%s status_reason must explain the cycle rejection", syncID)
		}
	}
}

func TestImportProject_CrossProjectSupersedeTargetRejected(t *testing.T) {
	dst := mustOpen(t)
	ctx := context.Background()

	sessOther := mustSession(t, dst, "other-proj")
	other := mustObservationForProject(t, dst, sessOther.ID, "other-proj")

	b := bundle.Bundle{
		Manifest: bundle.Manifest{FormatVersion: bundle.FormatVersion, Project: "proj-a"},
		Observations: []bundle.Observation{
			{
				SyncID: "obs-crossproj00001", Type: "decision", Title: "new one", Content: "content",
				Project: "proj-a", Scope: "project", NormalizedHash: "hash-x", RevisionCount: 1,
				LastSeenAt: "2026-01-01T00:00:00Z", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
				Status: store.StatusSuperseded, SupersededBySyncID: &other.SyncID,
			},
		},
	}

	report, err := dst.ImportProject(ctx, b, store.ImportOptions{TargetProject: "proj-a"})
	if err != nil {
		t.Fatalf("ImportProject: %v", err)
	}
	if report.Inserted != 1 {
		t.Fatalf("Inserted = %d, want 1", report.Inserted)
	}
	if len(report.StatusDowngrades) != 1 {
		t.Fatalf("StatusDowngrades = %d, want 1", len(report.StatusDowngrades))
	}
	dg := report.StatusDowngrades[0]
	if dg.SyncID != "obs-crossproj00001" || dg.Wanted != store.StatusSuperseded {
		t.Errorf("StatusDowngrade = %+v", dg)
	}
	if !strings.Contains(dg.Reason, "project") {
		t.Errorf("reason = %q, want it to mention the cross-project mismatch", dg.Reason)
	}

	localID := mustFindBySyncID(t, dst, ctx, "obs-crossproj00001")
	obs, err := dst.GetObservation(ctx, localID)
	if err != nil {
		t.Fatalf("GetObservation: %v", err)
	}
	if obs.Status != store.StatusActive {
		t.Errorf("status = %q, want active", obs.Status)
	}

	// The other-project target must be completely untouched.
	untouched, err := dst.GetObservation(ctx, other.ID)
	if err != nil {
		t.Fatalf("GetObservation(other): %v", err)
	}
	if untouched.Status != store.StatusActive || untouched.SupersededBy != nil {
		t.Errorf("cross-project target was mutated: status=%q superseded_by=%v", untouched.Status, untouched.SupersededBy)
	}
}

func TestImportProject_UnresolvedSupersedeBecomesActiveWithReason(t *testing.T) {
	src := mustOpen(t)
	dst := mustOpen(t)
	ctx := context.Background()

	sess := mustSession(t, src, "shared-proj")
	oldObs := mustObservationForProject(t, src, sess.ID, "shared-proj")
	newObs := mustObservationForProject(t, src, sess.ID, "shared-proj")
	if err := src.SetObservationStatus(ctx, oldObs.ID, store.StatusSuperseded, &newObs.ID, "replaced"); err != nil {
		t.Fatalf("SetObservationStatus: %v", err)
	}

	b, err := src.ExportProject(ctx, "shared-proj", store.ExportProjectOptions{})
	if err != nil {
		t.Fatalf("ExportProject: %v", err)
	}

	// Drop the superseding observation from the bundle to simulate a target
	// that will never resolve locally.
	filtered := b.Observations[:0]
	for _, o := range b.Observations {
		if o.SyncID != newObs.SyncID {
			filtered = append(filtered, o)
		}
	}
	b.Observations = filtered

	report, err := dst.ImportProject(ctx, b, store.ImportOptions{TargetProject: "shared-proj"})
	if err != nil {
		t.Fatalf("ImportProject: %v", err)
	}
	if len(report.UnresolvedSupersedes) != 1 {
		t.Fatalf("UnresolvedSupersedes = %d, want 1", len(report.UnresolvedSupersedes))
	}

	dstOldID := mustFindBySyncID(t, dst, ctx, oldObs.SyncID)
	dstOld, err := dst.GetObservation(ctx, dstOldID)
	if err != nil {
		t.Fatalf("GetObservation: %v", err)
	}
	if dstOld.Status != store.StatusActive {
		t.Errorf("status = %q, want active (unresolved supersede falls back to active)", dstOld.Status)
	}
	if dstOld.SupersededBy != nil {
		t.Errorf("superseded_by = %v, want nil", dstOld.SupersededBy)
	}
	if dstOld.StatusReason == nil || *dstOld.StatusReason == "" {
		t.Error("status_reason must explain the unresolved supersede")
	}
}

func TestImportProject_DryRunWritesNothing(t *testing.T) {
	src := mustOpen(t)
	dst := mustOpen(t)
	ctx := context.Background()

	sess := mustSession(t, src, "shared-proj")
	mustObservationForProject(t, src, sess.ID, "shared-proj")
	b, err := src.ExportProject(ctx, "shared-proj", store.ExportProjectOptions{})
	if err != nil {
		t.Fatalf("ExportProject: %v", err)
	}

	report, err := dst.ImportProject(ctx, b, store.ImportOptions{TargetProject: "shared-proj", DryRun: true})
	if err != nil {
		t.Fatalf("ImportProject dry-run: %v", err)
	}
	if report.Inserted != 1 {
		t.Errorf("Inserted = %d, want 1 (report reflects what WOULD happen)", report.Inserted)
	}

	obs, err := dst.RecentObservations(ctx, store.RecentObservationsParams{Project: "shared-proj"})
	if err != nil {
		t.Fatalf("RecentObservations: %v", err)
	}
	if len(obs) != 0 {
		t.Errorf("dry-run must not write: found %d observations", len(obs))
	}
}

func TestImportProject_ImportsPromptsWhenRequested(t *testing.T) {
	src := mustOpen(t)
	dst := mustOpen(t)
	ctx := context.Background()

	sess := mustSession(t, src, "shared-proj")
	mustPrompt(t, src, sess.ID, "hello teammate", "shared-proj")

	b, err := src.ExportProject(ctx, "shared-proj", store.ExportProjectOptions{IncludePrompts: true})
	if err != nil {
		t.Fatalf("ExportProject: %v", err)
	}

	report, err := dst.ImportProject(ctx, b, store.ImportOptions{TargetProject: "shared-proj", IncludePrompts: true})
	if err != nil {
		t.Fatalf("ImportProject: %v", err)
	}
	if report.Prompts != 1 {
		t.Errorf("Prompts = %d, want 1", report.Prompts)
	}

	prompts, err := dst.RecentPrompts(ctx, "shared-proj", 10)
	if err != nil {
		t.Fatalf("RecentPrompts: %v", err)
	}
	if len(prompts) != 1 || prompts[0].Content != "hello teammate" {
		t.Fatalf("prompts = %+v", prompts)
	}
}

func TestImportProject_FTSFindsImportedObservation(t *testing.T) {
	src := mustOpen(t)
	dst := mustOpen(t)
	ctx := context.Background()

	sess := mustSession(t, src, "shared-proj")
	obs, err := src.AddObservation(ctx, store.AddObservationParams{
		SessionID: sess.ID,
		Type:      "decision",
		Title:     "unique-searchable-title-xyz",
		Content:   "special unique searchable content marker",
		Project:   "shared-proj",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
	_ = obs

	b, err := src.ExportProject(ctx, "shared-proj", store.ExportProjectOptions{})
	if err != nil {
		t.Fatalf("ExportProject: %v", err)
	}
	if _, err := dst.ImportProject(ctx, b, store.ImportOptions{TargetProject: "shared-proj"}); err != nil {
		t.Fatalf("ImportProject: %v", err)
	}

	results, err := dst.Search(ctx, store.SearchParams{Q: "unique-searchable-title-xyz", Project: "shared-proj"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("search results = %d, want 1 (FTS index must be updated on import)", len(results))
	}
}

func TestImportProject_CrossProjectSyncIDConflict(t *testing.T) {
	dst := mustOpen(t)
	ctx := context.Background()

	sessA := mustSession(t, dst, "proj-a")
	obsA := mustObservationForProject(t, dst, sessA.ID, "proj-a")

	// A bundle claiming the SAME sync_id, but destined for a different project.
	b := bundle.Bundle{
		Manifest: bundle.Manifest{FormatVersion: bundle.FormatVersion, Project: "proj-b"},
		Observations: []bundle.Observation{
			{
				SyncID:         obsA.SyncID,
				Type:           "decision",
				Title:          "hijacked title",
				Content:        "hijacked content",
				Project:        "proj-b",
				Scope:          "project",
				NormalizedHash: "different-hash",
				RevisionCount:  1,
				LastSeenAt:     "2026-01-01T00:00:00Z",
				CreatedAt:      "2026-01-01T00:00:00Z",
				UpdatedAt:      "2026-01-01T00:00:00Z",
				Status:         "active",
			},
		},
	}

	report, err := dst.ImportProject(ctx, b, store.ImportOptions{TargetProject: "proj-b"})
	if err != nil {
		t.Fatalf("ImportProject: %v", err)
	}
	if report.Inserted != 0 {
		t.Errorf("Inserted = %d, want 0 (sync_id already exists under proj-a)", report.Inserted)
	}
	if report.Updated != 0 {
		t.Errorf("Updated = %d, want 0", report.Updated)
	}
	if len(report.Conflicts) != 1 {
		t.Fatalf("Conflicts = %d, want 1", len(report.Conflicts))
	}
	if report.Conflicts[0].SyncID != obsA.SyncID {
		t.Errorf("conflict sync_id = %q, want %q", report.Conflicts[0].SyncID, obsA.SyncID)
	}
	if !strings.Contains(report.Conflicts[0].Reason, "proj-a") {
		t.Errorf("conflict reason = %q, want it to mention proj-a", report.Conflicts[0].Reason)
	}

	// The original row must be completely untouched.
	unchanged, err := dst.GetObservation(ctx, obsA.ID)
	if err != nil {
		t.Fatalf("GetObservation: %v", err)
	}
	if unchanged.Title != obsA.Title || unchanged.Project != "proj-a" {
		t.Errorf("original row changed: title=%q project=%q", unchanged.Title, unchanged.Project)
	}

	// No duplicate row was inserted for that sync_id anywhere.
	var count int
	if err := dst.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM observations WHERE sync_id=?", obsA.SyncID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("rows with sync_id %q = %d, want 1 (no duplicate inserted)", obsA.SyncID, count)
	}
}

func TestImportProject_SecondIdenticalImportLeavesSessionCountUnchanged(t *testing.T) {
	src := mustOpen(t)
	dst := mustOpen(t)
	ctx := context.Background()

	sess := mustSession(t, src, "shared-proj")
	mustObservationForProject(t, src, sess.ID, "shared-proj")
	b, err := src.ExportProject(ctx, "shared-proj", store.ExportProjectOptions{})
	if err != nil {
		t.Fatalf("ExportProject: %v", err)
	}

	if _, err := dst.ImportProject(ctx, b, store.ImportOptions{TargetProject: "shared-proj"}); err != nil {
		t.Fatalf("first ImportProject: %v", err)
	}
	countAfterFirst := countSessions(t, dst)
	if countAfterFirst == 0 {
		t.Fatal("expected at least 1 session after an import that actually inserted a row")
	}

	// Re-importing the identical bundle: every row skips, nothing is
	// inserted, so no new "import-<timestamp>" session should be created.
	if _, err := dst.ImportProject(ctx, b, store.ImportOptions{TargetProject: "shared-proj"}); err != nil {
		t.Fatalf("second ImportProject: %v", err)
	}
	countAfterSecond := countSessions(t, dst)
	if countAfterSecond != countAfterFirst {
		t.Errorf("session count after no-op re-import = %d, want unchanged from %d", countAfterSecond, countAfterFirst)
	}
}

func TestImportProject_DryRunReportsSessionIDWithoutCreatingOne(t *testing.T) {
	src := mustOpen(t)
	dst := mustOpen(t)
	ctx := context.Background()

	sess := mustSession(t, src, "shared-proj")
	mustObservationForProject(t, src, sess.ID, "shared-proj")
	b, err := src.ExportProject(ctx, "shared-proj", store.ExportProjectOptions{})
	if err != nil {
		t.Fatalf("ExportProject: %v", err)
	}

	before := countSessions(t, dst)
	report, err := dst.ImportProject(ctx, b, store.ImportOptions{TargetProject: "shared-proj", DryRun: true})
	if err != nil {
		t.Fatalf("ImportProject dry-run: %v", err)
	}
	if report.SessionID == "" {
		t.Error("dry-run report must still describe the session that WOULD be created")
	}
	after := countSessions(t, dst)
	if after != before {
		t.Errorf("session count changed during dry-run: before=%d after=%d", before, after)
	}
}

func countSessions(t *testing.T, s *store.Store) int {
	t.Helper()
	var n int
	if err := s.DB().QueryRow("SELECT COUNT(*) FROM sessions").Scan(&n); err != nil {
		t.Fatalf("countSessions: %v", err)
	}
	return n
}

func TestImportProject_RequiresTargetProject(t *testing.T) {
	dst := mustOpen(t)
	ctx := context.Background()
	_, err := dst.ImportProject(ctx, bundle.Bundle{}, store.ImportOptions{})
	if err == nil {
		t.Fatal("expected error when TargetProject is empty")
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func mustFindByTitle(t *testing.T, s *store.Store, ctx context.Context, title string) int64 {
	t.Helper()
	obs, err := s.RecentObservations(ctx, store.RecentObservationsParams{Limit: 200})
	if err != nil {
		t.Fatalf("RecentObservations: %v", err)
	}
	for _, o := range obs {
		if o.Title == title {
			return o.ID
		}
	}
	t.Fatalf("no observation found with title %q", title)
	return 0
}

func mustFindBySyncID(t *testing.T, s *store.Store, ctx context.Context, syncID string) int64 {
	t.Helper()
	obs, err := s.RecentObservations(ctx, store.RecentObservationsParams{Limit: 200})
	if err != nil {
		t.Fatalf("RecentObservations: %v", err)
	}
	for _, o := range obs {
		if o.SyncID == syncID {
			return o.ID
		}
	}
	t.Fatalf("no observation found with sync_id %q", syncID)
	return 0
}
