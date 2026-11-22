package main

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type auditWriter interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func (a *app) audit(r *http.Request, db auditWriter, action, resource, target string) error {
	_, err := db.Exec(r.Context(), "INSERT INTO operation_audit(actor,action,resource,target) VALUES($1,$2,$3,$4)", a.user, action, resource, target)
	return err
}

func (a *app) identity(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !a.allowIdentity {
		if err := a.audit(r, a.db, "reveal_denied", "records", id); err != nil {
			return err
		}
		return problem{403, "当前账号无权查看完整证件号码"}
	}
	var value string
	err := a.db.QueryRow(r.Context(), "SELECT raw->>'IDNumber' FROM records WHERE id=$1 AND kind IN ('person','face') AND nullif(raw->>'IDNumber','') IS NOT NULL", id).Scan(&value)
	if err != nil {
		return err
	}
	if err = a.audit(r, a.db, "reveal", "records", id); err != nil {
		return err
	}
	return write(w, M{"value": value})
}

func (a *app) export(w http.ResponseWriter, r *http.Request) error {
	resource := r.PathValue("resource")
	view := managementViews[resource]
	if view == "" || resource == "audit" {
		return bad("资源无效")
	}
	var request struct{ IDs []string }
	if err := decode(r, &request); err != nil {
		return err
	}
	if len(request.IDs) == 0 || len(request.IDs) > 200 {
		return bad("请选择 1–200 条记录")
	}
	ctx := r.Context()
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := jsonRows(ctx, tx, "SELECT * FROM "+view+" WHERE id=ANY($1::text[]) ORDER BY array_position($1::text[],id)", request.IDs)
	if err != nil {
		return err
	}
	if len(rows) != len(request.IDs) {
		return pgx.ErrNoRows
	}
	_, err = tx.Exec(ctx, "INSERT INTO operation_audit(actor,action,resource,target) SELECT $1,'export',$2,id FROM unnest($3::text[]) ids(id)", a.user, resource, request.IDs)
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return write(w, M{"items": rows})
}
