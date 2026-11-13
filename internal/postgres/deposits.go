package postgres

import (
	"context"
	"errors"
	"fmt"
	"payment_ledger/internal/ledger"

	"github.com/jackc/pgx/v5"
)

func (r *AccountRepository) Deposit(ctx context.Context, userID, accountID string, amount int64) (ledger.Deposit, error) {
	var res ledger.Deposit
	err := inTx(ctx, r.pool, func(tx pgx.Tx) error {
		var err error
		res, err = r.DepositTx(ctx, tx, userID, accountID, amount)
		return err
	})
	if err != nil {
		return ledger.Deposit{}, err
	}
	return res, nil
}

func (r *AccountRepository) DepositTx(ctx context.Context, tx pgx.Tx, userID, accountID string, amount int64) (ledger.Deposit, error) {
	res := ledger.Deposit{AccountID: accountID, Amount: amount}

	var currency string

	err := tx.QueryRow(ctx, `SELECT currency FROM accounts WHERE id = $1 AND user_id = $2 FOR UPDATE`, accountID, userID).
		Scan(&currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return ledger.Deposit{}, fmt.Errorf("account %s: %w", accountID, ledger.ErrNotFound)
	}
	if err != nil {
		return ledger.Deposit{}, fmt.Errorf("lock account: %w", err)
	}

	var outsideID string
	err = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE type = 'system' AND code = 'outside' AND currency = $1`, currency).
		Scan(&outsideID)
	if err != nil {
		return ledger.Deposit{}, fmt.Errorf("find outside account: %w", err)
	}

	err = tx.QueryRow(ctx, `INSERT INTO operations (type) VALUES ('deposit') RETURNING id`).
		Scan(&res.OperationID)
	if err != nil {
		return ledger.Deposit{}, fmt.Errorf("insert operations: %w", err)
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO postings (operation_id, account_id, amount) VALUES ($1,$2,$3), ($1, $4, $5)`,
		res.OperationID, outsideID, -amount, accountID, amount)
	if err != nil {
		return ledger.Deposit{}, fmt.Errorf("insert postings: %w", err)
	}

	err = tx.QueryRow(ctx, `UPDATE accounts SET balance = balance + $1 WHERE id = $2 RETURNING balance`, amount, accountID).
		Scan(&res.Balance)
	if err != nil {
		return ledger.Deposit{}, fmt.Errorf("could not update deposit balance: %w", err)
	}

	return res, nil
}
