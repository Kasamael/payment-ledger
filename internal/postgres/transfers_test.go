package postgres

import (
	"cmp"
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"payment_ledger/internal/ledger"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultTestDatabaseURL = "postgres://app:app@127.0.0.1:5433/ledger?sslmode=disable"

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := cmp.Or(os.Getenv("TEST_DATABASE_URL"), defaultTestDatabaseURL)
	pool, err := Connect(ctx, url)
	if err != nil {
		t.Skipf("postgres is not available, skipping: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool
}

func newTestUser(t *testing.T, pool *pgxpool.Pool, balance int64) (userID, accountID string) {
	t.Helper()
	ctx := context.Background()

	userID = uuid.NewString()
	accountID = uuid.NewString()

	_, err := pool.Exec(ctx,
		`INSERT INTO users (id, first_name, last_name) VALUES ($1, 'Test', 'User')`, userID)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO accounts (id, user_id, type, currency, balance) VALUES ($1, $2, 'user', 'EUR', 0)`,
		accountID, userID)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	t.Cleanup(func() { deleteTestUser(t, pool, userID, accountID) })

	if balance > 0 {
		if _, err := NewAccountRepository(pool).Deposit(ctx, userID, accountID, balance); err != nil {
			t.Fatalf("deposit: %v", err)
		}
	}
	return userID, accountID
}

func deleteTestUser(t *testing.T, pool *pgxpool.Pool, userID, accountID string) {
	ctx := context.Background()

	rows, err := pool.Query(ctx, `SELECT DISTINCT operation_id FROM postings WHERE account_id = $1`, accountID)
	if err != nil {
		t.Errorf("cleanup: %v", err)
		return
	}
	opIDs, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		t.Errorf("cleanup: %v", err)
		return
	}

	steps := []struct {
		query string
		arg   any
	}{
		{`DELETE FROM postings WHERE operation_id = ANY($1)`, opIDs},
		{`DELETE FROM operations WHERE id = ANY($1)`, opIDs},
		{`DELETE FROM accounts WHERE id = $1`, accountID},
		{`DELETE FROM users WHERE id = $1`, userID},
	}
	for _, s := range steps {
		if _, err := pool.Exec(ctx, s.query, s.arg); err != nil {
			t.Errorf("cleanup %q: %v", s.query, err)
			return
		}
	}
}

func runAtOnce(fns ...func()) {
	var ready, done sync.WaitGroup
	start := make(chan struct{})
	for _, fn := range fns {
		ready.Add(1)
		done.Add(1)
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			fn()
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()
}

func balanceOf(t *testing.T, repo *AccountRepository, userID, accountID string) int64 {
	t.Helper()
	acc, err := repo.Get(context.Background(), userID, accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	return acc.Balance
}

func TestConcurrentTransfers(t *testing.T) {
	pool := testPool(t)
	repo := NewAccountRepository(pool)

	sender, from := newTestUser(t, pool, 10000)
	_, to := newTestUser(t, pool, 0)

	errs := make([]error, 2)
	runAtOnce(
		func() { _, errs[0] = repo.Transfer(context.Background(), sender, from, to, 8000) },
		func() { _, errs[1] = repo.Transfer(context.Background(), sender, from, to, 8000) },
	)

	var ok, refused int
	for i, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ledger.ErrInsufficientFunds):
			refused++
		default:
			t.Errorf("transfer %d: unexpected error: %v", i, err)
		}
	}
	if ok != 1 || refused != 1 {
		t.Fatalf("want 1 success and 1 insufficient funds, got %d and %d", ok, refused)
	}

	if got := balanceOf(t, repo, sender, from); got != 2000 {
		t.Errorf("sender balance: want 2000, got %d", got)
	}
}

func TestCrossingTransfers(t *testing.T) {
	pool := testPool(t)
	repo := NewAccountRepository(pool)

	userA, accA := newTestUser(t, pool, 5000)
	userB, accB := newTestUser(t, pool, 5000)

	const n = 25
	fns := make([]func(), 0, 2*n)
	errs := make([]error, 2*n)
	for i := range n {
		fns = append(fns,
			func() { _, errs[2*i] = repo.Transfer(context.Background(), userA, accA, accB, 10) },
			func() { _, errs[2*i+1] = repo.Transfer(context.Background(), userB, accB, accA, 10) },
		)
	}
	runAtOnce(fns...)

	for i, err := range errs {
		if err != nil {
			t.Errorf("transfer %d: %v", i, err)
		}
	}
	if a, b := balanceOf(t, repo, userA, accA), balanceOf(t, repo, userB, accB); a != 5000 || b != 5000 {
		t.Errorf("balances: want 5000 and 5000, got %d and %d", a, b)
	}
}

func TestTransferFromSomeoneElsesAccount(t *testing.T) {
	pool := testPool(t)
	repo := NewAccountRepository(pool)

	thief, thiefAcc := newTestUser(t, pool, 0)
	_, victimAcc := newTestUser(t, pool, 1000)

	_, err := repo.Transfer(context.Background(), thief, victimAcc, thiefAcc, 500)
	if !errors.Is(err, ledger.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
