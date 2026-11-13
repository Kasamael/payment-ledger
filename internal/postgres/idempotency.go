package postgres

import (
	"bytes"
	"context"
	"fmt"
	"payment_ledger/internal/ledger"

	"github.com/jackc/pgx/v5"
)

type Response struct {
	Status int
	Body   []byte
}

func (r *AccountRepository) Idempotent(ctx context.Context, userID, key string,
	requestHash []byte, work func(tx pgx.Tx) (Response, error)) (Response, error) {
	var resp Response
	err := inTx(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO idempotency_keys (user_id, key, request_hash) VALUES($1,$2,$3) 
ON CONFLICT(user_id, key) DO NOTHING`, userID, key, requestHash)
		if err != nil {
			return fmt.Errorf("claim idempotency key: %w", err)
		}

		if tag.RowsAffected() == 0 {
			var savedHash []byte
			err := tx.QueryRow(ctx, `SELECT request_hash, response_code, response_body 
FROM idempotency_keys WHERE user_id = $1 AND key = $2`, userID, key).
				Scan(&savedHash, &resp.Status, &resp.Body)
			if err != nil {
				return fmt.Errorf("read idempotency key: %w", err)
			}
			if !bytes.Equal(savedHash, requestHash) {
				return ledger.ErrIdempotencyKeyReused
			}
			return nil
		}

		resp, err = work(tx)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `UPDATE idempotency_keys SET response_code = $3, response_body = $4 WHERE user_id = $1 AND key = $2`,
			userID, key, resp.Status, resp.Body)
		if err != nil {
			return fmt.Errorf("save idempotency response: %w", err)
		}
		return nil
	})
	if err != nil {
		return Response{}, err
	}
	return resp, nil
}
