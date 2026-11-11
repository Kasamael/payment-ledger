package postgres

import (
	"context"
	"errors"
	"fmt"
	"payment_ledger/internal/ledger"

	"github.com/jackc/pgx/v5"
)

type lockedAccount struct {
	userID   string
	currency string
	balance  int64
}

func (r *AccountRepository) Transfer(ctx context.Context, userID, fromID, toID string, amount int64) (ledger.Transfer, error) {
	res := ledger.Transfer{FromAccountID: fromID, ToAccountID: toID, Amount: amount}
	err := inTx(ctx, r.pool, func(tx pgx.Tx) error {
		first, second := fromID, toID
		if second < first {
			first, second = second, first
		}
		a1, err := lockAccount(ctx, tx, first)
		if err != nil {
			return err
		}

		a2, err := lockAccount(ctx, tx, second)
		if err != nil {
			return err
		}

		from, to := a1, a2
		if fromID != first {
			from, to = a2, a1
		}

		if from.userID != userID {
			return fmt.Errorf("account %s: %w", fromID, err)
		}
		if from.currency != to.currency {
			return ledger.ErrCurrencyMismatch
		}
		if from.balance < amount {
			return ledger.ErrInsufficientFunds
		}

		err = tx.QueryRow(ctx, "INSERT INTO operations (type) VALUES ('transfer') RETURNING id").Scan(&res.OperationID)
		if err != nil {
			return fmt.Errorf("insert operation: %w", err)
		}

		_, err = tx.Exec(ctx, "INSERT INTO postings (operation_id, account_id, amount) VALUES ($1, $2, $3), ($1, $4, $5)",
			res.OperationID, fromID, -amount, toID, amount)
		if err != nil {
			return fmt.Errorf("insert postings: %w", err)
		}

		err = tx.QueryRow(ctx, "UPDATE accounts SET balance = balance - $1 WHERE id = $2 RETURNING balance", amount, fromID).
			Scan(&res.Balance)
		if err != nil {
			return fmt.Errorf("update sender balance: %w", err)
		}

		_, err = tx.Exec(ctx, "UPDATE accounts SET balance = balance + $1 WHERE id = $2", amount, toID)
		if err != nil {
			return fmt.Errorf("update recipient balance: %w", err)
		}
		return nil
	})
	if err != nil {
		return ledger.Transfer{}, err
	}
	return res, nil
}

func lockAccount(ctx context.Context, tx pgx.Tx, accountID string) (lockedAccount, error) {
	var a lockedAccount
	err := tx.QueryRow(ctx, "SELECT user_id, currency, balance FROM accounts WHERE id = $1 AND type = 'user' FOR UPDATE", accountID).
		Scan(&a.userID, &a.currency, &a.balance)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedAccount{}, fmt.Errorf("account %s: %w", accountID, ledger.ErrNotFound)
	}
	if err != nil {
		return lockedAccount{}, fmt.Errorf("lock account %s: %w", accountID, ledger.ErrNotFound)
	}
	return a, nil
}
