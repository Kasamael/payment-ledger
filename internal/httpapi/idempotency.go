package httpapi

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/Kasamael/payment-ledger/internal/ledger"
	"github.com/Kasamael/payment-ledger/internal/postgres"
)

const (
	maxKeyLength = 255
	maxBodyBytes = 1 << 20
)

func readIdempotency(w http.ResponseWriter, r *http.Request) (key string, body, hash []byte, ok bool) {
	key = r.Header.Get("Idempotency-Key")
	if key == "" || len(key) > maxKeyLength {
		writeResponse(w, http.StatusBadRequest, errorBody("Idemptency-Key header is required, up to 255 characters"))
		return "", nil, nil, false
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		writeResponse(w, http.StatusInternalServerError, errorBody("cannot read body"))
		return "", nil, nil, false
	}

	sum := sha256.Sum256([]byte(r.Method + " " + r.URL.Path + "\n" + string(body)))
	return key, body, sum[:], true
}

func businessError(err error) (status int, message string, ok bool) {
	switch {
	case errors.Is(err, ledger.ErrNotFound):
		return http.StatusNotFound, "account not found", true
	case errors.Is(err, ledger.ErrInsufficientFunds):
		return http.StatusUnprocessableEntity, "insufficient funds", true
	case errors.Is(err, ledger.ErrCurrencyMismatch):
		return http.StatusUnprocessableEntity, "currency mismatch", true
	case errors.Is(err, ledger.ErrIdempotencyKeyReused):
		return http.StatusUnprocessableEntity, "idempotency key reused with a different request", true
	}
	return 0, "", false
}

func businessResponse(status int, value any, err error) (postgres.Response, error) {
	if err != nil {
		code, message, ok := businessError(err)
		if !ok {
			return postgres.Response{}, err
		}
		status, value = code, errorBody(message)
	}

	body, err := json.Marshal(value)
	if err != nil {
		return postgres.Response{}, err
	}
	return postgres.Response{Status: status, Body: body}, nil
}

func writeSaved(w http.ResponseWriter, resp postgres.Response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.Status)
	w.Write(resp.Body)
}
