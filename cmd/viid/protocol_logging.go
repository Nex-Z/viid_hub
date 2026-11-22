package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// protocolBatchFailure records the failing item without copying its identifiers or payload.
type protocolBatchFailure struct {
	cause       error
	stage       string
	item        int // one-based, or zero outside an item
	transaction string
}

func (f *protocolBatchFailure) Error() string { return f.cause.Error() }
func (f *protocolBatchFailure) Unwrap() error { return f.cause }

func (f *protocolBatchFailure) finish(ctx context.Context, tx pgx.Tx, result *error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := tx.Rollback(ctx)
	if *result == nil {
		return
	}
	switch {
	case err == nil:
		f.transaction = "rolled_back"
	case errors.Is(err, pgx.ErrTxClosed) && f.stage == "response":
		f.transaction = "committed"
	case errors.Is(err, pgx.ErrTxClosed):
		f.transaction = "closed"
	default:
		f.transaction = "rollback_unconfirmed"
	}
	f.cause = *result
	*result = f
}

func logProtocolRejection(w http.ResponseWriter, r *http.Request, code int, err error) {
	// The first unauthenticated Digest request is part of the normal handshake.
	if code == http.StatusUnauthorized && r.Header.Get("Authorization") == "" {
		return
	}
	collection, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/VIID/"), "/")
	switch collection {
	case "System", "APEs", "APSs", "Lanes", "Tollgates", "Persons", "Faces", "MotorVehicles", "NonMotorVehicles", "Images", "Subscribes", "SubscribeNotifications":
	default:
		collection = "unknown"
	}
	id := rand.Text()
	w.Header().Set("X-Request-ID", id)
	detail := ""
	var failure *protocolBatchFailure
	if errors.As(err, &failure) {
		detail = fmt.Sprintf(" stage=%s item=%d transaction=%s", failure.stage, failure.item, failure.transaction)
		err = failure.cause
	}
	if code >= 500 {
		detail += fmt.Sprintf(" error_type=%T", err)
	}
	var dbErr *pgconn.PgError
	if errors.As(err, &dbErr) {
		detail += fmt.Sprintf(" sqlstate=%q", dbErr.SQLState())
	}
	log.Printf("viid_rejected request_id=%s collection=%s http=%d%s", id, collection, code, detail)
}
