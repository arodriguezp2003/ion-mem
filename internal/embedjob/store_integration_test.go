package embedjob_test

// store_integration_test.go proves that *store.Store actually satisfies
// embedjob.Store (compile-time check) and exercises Run() against a real
// temporary SQLite-backed store, mirroring the setup used throughout
// internal/store and internal/tui's own embedding tests.

import (
	"context"
	"fmt"
	"testing"

	"github.com/arodriguezp2003/ion-mem/internal/embedjob"
	"github.com/arodriguezp2003/ion-mem/internal/store"
)

// Compile-time check: *store.Store must satisfy embedjob.Store.
var _ embedjob.Store = (*store.Store)(nil)

func TestRun_AgainstRealStore(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()
	if _, err := st.CreateSession(ctx, store.CreateSessionParams{
		ID: "embedjob-sess", Project: "embedjob-proj", Directory: "/embedjob",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := st.AddObservation(ctx, store.AddObservationParams{
			SessionID: "embedjob-sess", Type: "manual",
			Title: fmt.Sprintf("obs-%d", i), Content: "content",
			Project: "embedjob-proj", Scope: "project",
		}); err != nil {
			t.Fatalf("AddObservation %d: %v", i, err)
		}
	}

	fe := alwaysOK("real-store-model")
	ch := embedjob.Run(ctx, st, fe, nil, embedjob.Config{
		Project: "embedjob-proj",
		Batch:   10,
		Sleep:   noSleep,
	})
	events := drain(ch)

	last := events[len(events)-1]
	if last.Type != embedjob.EventDone {
		t.Fatalf("last event type = %v, want EventDone", last.Type)
	}
	if last.Summary.Status != embedjob.StatusComplete {
		t.Errorf("Status = %v, want StatusComplete", last.Summary.Status)
	}
	if last.Summary.Done != 3 || last.Summary.Total != 3 {
		t.Errorf("Done/Total = %d/%d, want 3/3", last.Summary.Done, last.Summary.Total)
	}

	have, total, err := st.EmbeddingCoverage(ctx, "embedjob-proj", "real-store-model")
	if err != nil {
		t.Fatalf("EmbeddingCoverage: %v", err)
	}
	if have != 3 || total != 3 {
		t.Errorf("store coverage = %d/%d, want 3/3", have, total)
	}
}
