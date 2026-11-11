package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"payment_ledger/internal/ledger"
	"payment_ledger/internal/postgres"

	"github.com/google/uuid"
)

type AccountHandler struct {
	accountRepository *postgres.AccountRepository
	logger            *slog.Logger
}

func NewAccountHandler(accountRepository *postgres.AccountRepository, logger *slog.Logger) *AccountHandler {
	return &AccountHandler{accountRepository: accountRepository, logger: logger}
}

func (h *AccountHandler) GetAccount(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromHeader(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}

	acc, err := h.accountRepository.Get(r.Context(), userID, id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeResponse(w, http.StatusOK, acc)
}

func (h *AccountHandler) GetAccounts(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromHeader(w, r)
	if !ok {
		return
	}

	accs, err := h.accountRepository.GetList(r.Context(), userID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeResponse(w, http.StatusOK, map[string]any{"accounts": accs})
}

func (h *AccountHandler) GetAccountPostings(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromHeader(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}

	if _, err := h.accountRepository.Get(r.Context(), userID, id); err != nil {
		h.writeError(w, r, err)
		return
	}

	postings, err := h.accountRepository.GetAccountPostings(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeResponse(w, http.StatusOK, map[string]any{"postings": postings})
}

func (h *AccountHandler) Deposit(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromHeader(w, r)
	if !ok {
		return
	}

	accountID, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}

	var body struct {
		Amount int64 `json:"amount"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&body); err != nil {
		writeResponse(w, http.StatusBadRequest, errorBody("invalid JSON body"))
		return
	}

	if body.Amount <= 0 {
		writeResponse(w, http.StatusBadRequest, errorBody("amount must be positive"))
		return
	}

	dep, err := h.accountRepository.Deposit(r.Context(), userID, accountID, body.Amount)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeResponse(w, http.StatusCreated, dep)
}

func (h *AccountHandler) Transfer(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromHeader(w, r)
	if !ok {
		return
	}

	var body struct {
		OperationID   int64  `json:"operation_id"`
		FromAccountID string `json:"from_account_id"`
		ToAccountID   string `json:"to_account_id"`
		Amount        int64  `json:"amount"`
	}

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeResponse(w, http.StatusBadRequest, errorBody("invalid JSON body"))
		return
	}

	fromID, errFrom := uuid.Parse(body.FromAccountID)
	toID, errTo := uuid.Parse(body.ToAccountID)
	if errFrom != nil || errTo != nil {
		writeResponse(w, http.StatusBadRequest, errorBody("account ids must be a valid UUIDs"))
		return
	}

	if fromID == toID {
		writeResponse(w, http.StatusBadRequest, errorBody("cannot transfer to same account"))
		return
	}

	if body.Amount <= 0 {
		writeResponse(w, http.StatusBadRequest, errorBody("amount must be positive"))
		return
	}

	transfer, err := h.accountRepository.Transfer(r.Context(), userID, fromID.String(), toID.String(), body.Amount)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	writeResponse(w, http.StatusCreated, transfer)
}

func (h *AccountHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ledger.ErrNotFound):
		writeResponse(w, http.StatusNotFound, errorBody("account not found"))
	case errors.Is(err, ledger.ErrCurrencyMismatch):
		writeResponse(w, http.StatusUnprocessableEntity, errorBody("currency mismatch"))
	case errors.Is(err, ledger.ErrInsufficientFunds):
		writeResponse(w, http.StatusUnprocessableEntity, errorBody("insufficient funds"))
	default:
		h.logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		writeResponse(w, http.StatusInternalServerError, errorBody("internal error"))
	}
}

func userIDFromHeader(w http.ResponseWriter, r *http.Request) (string, bool) {
	id, err := uuid.Parse(r.Header.Get("X-User-ID"))
	if err != nil {
		writeResponse(w, http.StatusBadRequest, errorBody("X-User-ID header must be a UUID"))
		return "", false
	}
	return id.String(), true
}

func pathUUID(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		writeResponse(w, http.StatusBadRequest, errorBody(name+" must be a UUID"))
		return "", false
	}
	return id.String(), true
}

func errorBody(message string) map[string]string {
	return map[string]string{"error": message}
}

func writeResponse(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(body)
}
