package service

import (
	"context"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"

	apperrors "github.com/activialtd/gomarketi.com-backend/shared/pkg/errors"
	orddb "github.com/activialtd/gomarketi.com-backend/services/orders/db"
)

// testDB connects to a local Postgres and applies migrations, or skips the
// test if no DATABASE_URL is available — this package has no existing test
// suite, so this is the first (and currently only) automated safety net for
// the payment idempotency guard.
func testDB(t *testing.T) *sqlx.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("DATABASE_URL/TEST_DATABASE_URL not set — skipping DB-backed test")
	}

	db, err := sqlx.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := db.PingContext(context.Background()); err != nil {
		t.Skipf("db unreachable: %v", err)
	}
	if err := orddb.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// TestClaimPaymentReference_RejectsReplay confirms a payment_reference can
// only ever be claimed once — the core guarantee the multi-store checkout
// idempotency design depends on. A second claim of the same reference,
// after the first has committed, must be detected as a conflict rather than
// silently succeeding (which would allow duplicate wallet credits).
func TestClaimPaymentReference_RejectsReplay(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	ref := "TEST_REF_" + t.Name()

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM checkout_payments WHERE payment_reference=$1`, ref)
	})

	// First claim commits successfully.
	tx1, err := db.BeginTxx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx1: %v", err)
	}
	if err := claimPaymentReference(ctx, tx1, ref, 10000); err != nil {
		t.Fatalf("first claim should succeed, got: %v", err)
	}
	if err := tx1.Commit(); err != nil {
		t.Fatalf("commit tx1: %v", err)
	}

	// Second claim of the same reference, in a fresh transaction, must be
	// rejected as a conflict — this is what makes a replayed payment safe.
	tx2, err := db.BeginTxx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx2: %v", err)
	}
	defer tx2.Rollback() //nolint:errcheck

	err = claimPaymentReference(ctx, tx2, ref, 10000)
	if err == nil {
		t.Fatal("second claim of the same payment_reference should fail, got nil error")
	}
	if !apperrors.IsConflict(err) {
		t.Fatalf("expected a conflict error, got: %v", err)
	}
}

// TestClaimPaymentReference_ConcurrentInsertsSerialize exercises the actual
// DB-level serialization two concurrent requests for the same reference
// rely on — not just sequential replay. Exactly one of two simultaneous
// claims must succeed.
func TestClaimPaymentReference_ConcurrentInsertsSerialize(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	ref := "TEST_CONCURRENT_" + t.Name()

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM checkout_payments WHERE payment_reference=$1`, ref)
	})

	results := make(chan error, 2)
	claim := func() {
		tx, err := db.BeginTxx(ctx, nil)
		if err != nil {
			results <- err
			return
		}
		if err := claimPaymentReference(ctx, tx, ref, 5000); err != nil {
			_ = tx.Rollback()
			results <- err
			return
		}
		results <- tx.Commit()
	}

	go claim()
	go claim()

	successes := 0
	for i := 0; i < 2; i++ {
		if err := <-results; err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("expected exactly 1 of 2 concurrent claims to succeed, got %d", successes)
	}
}
