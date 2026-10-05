//go:build integration

package postgres_test

import (
	"musicgetter/internal/telegram"
	"testing"
)

func TestSingleTelegramPollerPerBot(t *testing.T) {
	f := newFixture(t)
	first, err := f.pool.Acquire(f.ctx)
	requireOK(t, err)
	defer first.Release()
	second, err := f.pool.Acquire(f.ctx)
	requireOK(t, err)
	defer second.Release()
	requireOK(t, telegram.AcquirePollLock(f.ctx, first.Conn(), 90001))
	defer first.Exec(f.ctx, `SELECT pg_advisory_unlock_all()`)
	if err = telegram.AcquirePollLock(f.ctx, second.Conn(), 90001); err == nil {
		t.Fatal("second poller acquired lock")
	}
	requireOK(t, telegram.AcquirePollLock(f.ctx, second.Conn(), 90002))
	defer second.Exec(f.ctx, `SELECT pg_advisory_unlock_all()`)
	_, err = first.Exec(f.ctx, `SELECT pg_advisory_unlock_all()`)
	requireOK(t, err)
	requireOK(t, telegram.AcquirePollLock(f.ctx, second.Conn(), 90001))
}
