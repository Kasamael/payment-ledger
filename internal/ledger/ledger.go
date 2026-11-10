package ledger

import (
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("not found")
)

type Account struct {
	ID        string    `json:"id" db:"id"`
	Currency  string    `json:"currency" db:"currency"`
	Balance   int64     `json:"balance" db:"balance"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

type Posting struct {
	ID          int64     `json:"id" db:"id"`
	OperationID int64     `json:"operation_id" db:"operation_id"`
	Type        string    `json:"type" db:"type"`
	Amount      int64     `json:"amount" db:"amount"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}

type Deposit struct {
	OperationID int64  `json:"operation_id"`
	AccountID   string `json:"account_id"`
	Amount      int64  `json:"amount"`
	Balance     int64  `json:"balance"`
}
