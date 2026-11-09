-- +goose Up
CREATE TABLE users
(
    id         uuid PRIMARY KEY     DEFAULT gen_random_uuid(),
    first_name text        NOT NULL,
    last_name  text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE accounts
(
    id         uuid PRIMARY KEY     DEFAULT gen_random_uuid(),
    user_id    uuid REFERENCES users (id),
    type       text        NOT NULL CHECK ( type IN ('user', 'system')),
    code       text CHECK ( code IN ('frozen', 'outside') ),
    currency   char(3)     NOT NULL,
    balance    bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT accounts_kind_check CHECK (
        (type = 'user' AND user_id IS NOT NULL AND code IS NULL AND balance IS NOT NULL)
            OR (type = 'system' AND user_id IS NULL AND code IS NOT NULL AND balance IS NULL)),
    CONSTRAINT accounts_balance_non_negative CHECK (balance >= 0),
    UNIQUE (code, currency),
    CONSTRAINT accounts_user_currency_unique UNIQUE (user_id, currency)
);


CREATE TABLE operations
(
    id        bigserial PRIMARY KEY,
    type      text        NOT NULL CHECK (type IN
                                          ('deposit', 'transfer', 'payout_hold', 'payout_settle', 'payout_release')),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE postings
(
    id           bigserial PRIMARY KEY,
    operation_id bigint      NOT NULL REFERENCES operations (id),
    account_id   uuid        NOT NULL REFERENCES accounts (id),
    amount       bigint      NOT NULL CHECK ( amount <> 0 ),
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX postings_account_id_id_idx ON postings (account_id, id);

CREATE TABLE idempotency_keys
(
    user_id       uuid        NOT NULL REFERENCES users (id),
    key           text        NOT NULL,
    request_hash  bytea       NOT NULL,
    response_code int,
    response_body jsonb,
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, key)
);

CREATE INDEX idempotency_keys_created_at_idx ON idempotency_keys (created_at);

-- +goose Down
DROP TABLE idempotency_keys;
DROP TABLE postings;
DROP TABLE operations;
DROP TABLE accounts;
DROP TABLE users;