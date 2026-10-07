//go:build integration

package postgres_test

import (
	"context"
	"musicgetter/internal/destination"
	"musicgetter/internal/domain"
	importer "musicgetter/internal/import"
	"musicgetter/internal/storage/postgres"
	"musicgetter/migrations"
	"testing"
	"time"
)

type atomicOnly struct{ d *atomicDestination }

func (a atomicOnly) Capabilities(ctx context.Context) (destination.Capabilities, error) {
	c, e := a.d.Capabilities(ctx)
	c.ReadMembership = false
	return c, e
}
func (a atomicOnly) EnsureTrack(ctx context.Context, r destination.EnsureRequest) (destination.Effect, error) {
	return a.d.EnsureTrack(ctx, r)
}

type reconciledAtomic struct {
	atomicOnly
	effect destination.EffectState
}

func (a reconciledAtomic) Capabilities(ctx context.Context) (destination.Capabilities, error) {
	c, e := a.atomicOnly.Capabilities(ctx)
	c.ReconcileOperations = true
	return c, e
}
func (a reconciledAtomic) Reconcile(context.Context, string, string) (destination.Effect, error) {
	return destination.Effect{State: a.effect}, nil
}
func TestPipelineUnknownAtomicRetryAndPendingReconciliation(t *testing.T) {
	f := newFixture(t)
	d := newAtomic()
	d.loseACK = true
	p := pipeline(f, d)
	var remote destination.Destination = atomicOnly{d}
	p.Resolver = importer.Registry{"fake": func(context.Context, domain.DestinationConnection) (importer.Binding, error) {
		return importer.Binding{Destination: remote, Catalog: d}, nil
	}}
	i := f.importRecord(t, "unknown")
	f.enqueue(t, f.item(t, i, "Song"), 4)
	j := claimOne(t, f)
	if err := p.Process(f.ctx, j); err == nil {
		t.Fatal("expected uncertain send")
	}
	requireOK(t, p.Store.Reschedule(f.ctx, j, time.Now(), "uncertain", false))
	j = claimOne(t, f)
	remote = reconciledAtomic{atomicOnly{d}, destination.EffectPending}
	if err := p.Process(f.ctx, j); err == nil {
		t.Fatal("pending operation finished")
	}
	if d.calls != 1 {
		t.Fatal("pending operation resent")
	}
	requireOK(t, p.Store.Reschedule(f.ctx, j, time.Now(), "pending", false))
	j = claimOne(t, f)
	remote = reconciledAtomic{atomicOnly{d}, destination.EffectUnknown}
	requireOK(t, p.Process(f.ctx, j))
	assertState(t, f, i.ID, domain.ImportCompleted)
	if d.adds != 1 || d.calls != 2 {
		t.Fatal("atomic retry duplicated effect", d.adds, d.calls)
	}
}

func TestPipelineMigrationPreservesInFlightIntent(t *testing.T) {
	f := newFixture(t)
	i := f.importRecord(t, "upgrade")
	item := f.item(t, i, "Song")
	remote := f.remote(t, "song")
	f.enqueue(t, item, 3)
	ledger, err := postgres.NewMembershipRepository(f.pool).Reserve(f.ctx, f.user.ID, f.connection.ID, f.destination.ID, remote.ID)
	requireOK(t, err)
	_, err = postgres.NewMembershipRepository(f.pool).Transition(f.ctx, f.user.ID, ledger.ID, domain.MembershipReserved, domain.MembershipUnknown)
	requireOK(t, err)
	requireOK(t, postgres.Migrate(f.ctx, f.cfg, migrations.FS, "down"))
	_, err = f.pool.Exec(f.ctx, `UPDATE musicgetter.imports SET state='running' WHERE id=$1`, i.ID)
	requireOK(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE musicgetter.import_items SET state='ensuring',destination_track_id=$2 WHERE id=$1`, item.ID, remote.ID)
	requireOK(t, err)
	requireOK(t, postgres.Migrate(f.ctx, f.cfg, migrations.FS, "up"))
	assertState(t, f, i.ID, domain.ImportProcessing)
	items, err := postgres.NewImportRepository(f.pool).ListItems(f.ctx, f.user.ID, i.ID, nil, 10)
	requireOK(t, err)
	if items[0].State != domain.ItemMatched {
		t.Fatal("lost checkpoint")
	}
	after, err := postgres.NewMembershipRepository(f.pool).Get(f.ctx, f.user.ID, f.destination.ID, remote.ID)
	requireOK(t, err)
	if after.OperationKey != ledger.OperationKey || after.State != domain.MembershipUnknown {
		t.Fatal("migration lost uncertain operation")
	}
}
