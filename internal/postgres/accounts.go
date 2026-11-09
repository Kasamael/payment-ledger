package postgres

import (
	"context"
	"errors"
	"fmt"
	"payment_ledger/internal/ledger"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const postingsLimit = 50

type AccountRepository struct {
	pool *pgxpool.Pool
}

func NewAccountRepository(pool *pgxpool.Pool) *AccountRepository {
	return &AccountRepository{pool}
}

func (r *AccountRepository) Get(ctx context.Context, userID, id string) (ledger.Account, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	const query = `SELECT id, currency, balance, created_at FROM accounts WHERE id = $1 AND user_id = $2`

	var acc ledger.Account

	err := r.pool.QueryRow(ctx, query, id, userID).
		Scan(&acc.ID, &acc.Currency, &acc.Balance, &acc.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ledger.Account{}, fmt.Errorf("account %s: %w", id, ledger.ErrNotFound)
	}
	if err != nil {
		return ledger.Account{}, fmt.Errorf("get account: %w", err)
	}
	return acc, nil
}

func (r *AccountRepository) GetList(ctx context.Context, userID string) ([]ledger.Account, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	const query = `SELECT id, currency, balance, created_at FROM accounts WHERE user_id = $1 ORDER BY currency`

	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("GetList accounts: %w", err)
	}

	accounts, err := pgx.CollectRows(rows, pgx.RowToStructByName[ledger.Account])
	if err != nil {
		return nil, fmt.Errorf("GetList accounts: %w", err)
	}
	return accounts, nil
}

func (r *AccountRepository) GetAccountPostings(ctx context.Context, accountID string) ([]ledger.Posting, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	const query = `SELECT p.id, p.operation_id, o.type, p.amount, p.created_at
					FROM postings AS p
					INNER JOIN operations AS o ON o.id = p.operation_id
					WHERE p.account_id = $1
					ORDER BY p.id DESC
					LIMIT $2`
	rows, err := r.pool.Query(ctx, query, accountID, postingsLimit)
	if err != nil {
		return nil, fmt.Errorf("GetAccountPostings: %w", err)
	}

	postings, err := pgx.CollectRows(rows, pgx.RowToStructByName[ledger.Posting])
	if err != nil {
		return nil, fmt.Errorf("GetAccountPostings: %w", err)
	}
	return postings, nil
}
