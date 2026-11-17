package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Kasamael/payment-ledger/internal/ledger"
)

func countingDeposit(repo *AccountRepository, calls *atomic.Int32, userID, accountID string, amount int64) func(pgx.Tx) (Response, error) {
	return func(tx pgx.Tx) (Response, error) {
		calls.Add(1)
		dep, err := repo.DepositTx(context.Background(), tx, userID, accountID, amount)
		if err != nil {
			return Response{}, err
		}
		body := fmt.Sprintf(`{"operation_id":%d,"balance":%d}`, dep.OperationID, dep.Balance)
		return Response{Status: http.StatusCreated, Body: []byte(body)}, nil
	}
}

func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	var va, vb any
	if err := json.Unmarshal(a, &va); err != nil {
		t.Fatalf("bad JSON %s: %v", a, err)
	}
	if err := json.Unmarshal(b, &vb); err != nil {
		t.Fatalf("bad JSON %s: %v", b, err)
	}
	return reflect.DeepEqual(va, vb)
}

func TestIdempotentReplay(t *testing.T) {
	pool := testPool(t)
	repo := NewAccountRepository(pool)
	ctx := context.Background()
	user, acc := newTestUser(t, pool, 0)

	key, hash := uuid.NewString(), []byte("deposit 100")
	var calls atomic.Int32

	first, err := repo.Idempotent(ctx, user, key, hash, countingDeposit(repo, &calls, user, acc, 100))
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	again, err := repo.Idempotent(ctx, user, key, hash, countingDeposit(repo, &calls, user, acc, 100))
	if err != nil {
		t.Fatalf("again: %v", err)
	}

	if calls.Load() != 1 {
		t.Errorf("work ran %d times, want 1", calls.Load())
	}
	if first.Status != again.Status {
		t.Errorf("status: first %d, again %d", first.Status, again.Status)
	}
	// jsonb may reorder keys and add spaces, so compare the meaning, not the bytes.
	if !sameJSON(t, first.Body, again.Body) {
		t.Errorf("body: first %s, again %s", first.Body, again.Body)
	}
	if got := balanceOf(t, repo, user, acc); got != 100 {
		t.Errorf("balance: want 100, got %d", got)
	}
}

func TestIdempotentKeyReusedWithDifferentRequest(t *testing.T) {
	pool := testPool(t)
	repo := NewAccountRepository(pool)
	ctx := context.Background()
	user, acc := newTestUser(t, pool, 0)

	key := uuid.NewString()
	var calls atomic.Int32

	if _, err := repo.Idempotent(ctx, user, key, []byte("deposit 100"), countingDeposit(repo, &calls, user, acc, 100)); err != nil {
		t.Fatalf("first: %v", err)
	}
	_, err := repo.Idempotent(ctx, user, key, []byte("deposit 999"), countingDeposit(repo, &calls, user, acc, 999))
	if !errors.Is(err, ledger.ErrIdempotencyKeyReused) {
		t.Fatalf("want ErrIdempotencyKeyReused, got %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("work ran %d times, want 1", calls.Load())
	}
	if got := balanceOf(t, repo, user, acc); got != 100 {
		t.Errorf("balance: want 100, got %d", got)
	}
}

func TestIdempotentRemembersRefusal(t *testing.T) {
	pool := testPool(t)
	repo := NewAccountRepository(pool)
	ctx := context.Background()
	user, acc := newTestUser(t, pool, 0)

	key, hash := uuid.NewString(), []byte("transfer 5000")
	refuse := func(pgx.Tx) (Response, error) {
		return Response{Status: http.StatusUnprocessableEntity, Body: []byte(`{"error":"insufficient funds"}`)}, nil
	}
	if _, err := repo.Idempotent(ctx, user, key, hash, refuse); err != nil {
		t.Fatalf("first: %v", err)
	}

	var calls atomic.Int32
	again, err := repo.Idempotent(ctx, user, key, hash, countingDeposit(repo, &calls, user, acc, 5000))
	if err != nil {
		t.Fatalf("again: %v", err)
	}
	if again.Status != http.StatusUnprocessableEntity {
		t.Errorf("status: want 422, got %d", again.Status)
	}
	if calls.Load() != 0 {
		t.Errorf("work ran %d times on retry, want 0", calls.Load())
	}
}

func TestIdempotentRollsBackOnFailure(t *testing.T) {
	pool := testPool(t)
	repo := NewAccountRepository(pool)
	ctx := context.Background()
	user, acc := newTestUser(t, pool, 0)

	key, hash := uuid.NewString(), []byte("deposit 100")
	fail := func(pgx.Tx) (Response, error) { return Response{}, errors.New("database went away") }
	if _, err := repo.Idempotent(ctx, user, key, hash, fail); err == nil {
		t.Fatal("want an error from the failing work")
	}

	var calls atomic.Int32
	resp, err := repo.Idempotent(ctx, user, key, hash, countingDeposit(repo, &calls, user, acc, 100))
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if calls.Load() != 1 || resp.Status != http.StatusCreated {
		t.Errorf("retry: work ran %d times, status %d; want 1 and 201", calls.Load(), resp.Status)
	}
}

func TestIdempotentConcurrent(t *testing.T) {
	pool := testPool(t)
	repo := NewAccountRepository(pool)
	ctx := context.Background()
	user, acc := newTestUser(t, pool, 0)

	key, hash := uuid.NewString(), []byte("deposit 100")
	var calls atomic.Int32
	resps := make([]Response, 2)
	errs := make([]error, 2)
	runAtOnce(
		func() {
			resps[0], errs[0] = repo.Idempotent(ctx, user, key, hash, countingDeposit(repo, &calls, user, acc, 100))
		},
		func() {
			resps[1], errs[1] = repo.Idempotent(ctx, user, key, hash, countingDeposit(repo, &calls, user, acc, 100))
		},
	)

	for i, err := range errs {
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("work ran %d times, want 1", calls.Load())
	}
	if !sameJSON(t, resps[0].Body, resps[1].Body) {
		t.Errorf("bodies differ: %s vs %s", resps[0].Body, resps[1].Body)
	}
	if got := balanceOf(t, repo, user, acc); got != 100 {
		t.Errorf("balance: want 100, got %d", got)
	}
}
