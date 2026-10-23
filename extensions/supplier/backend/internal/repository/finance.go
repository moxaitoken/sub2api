package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"math"
)

var _ ports.FinanceStore = (*Store)(nil)

const settlementColumns = `s.id::text,l.id::text,s.supplier_id::text,s.supplier_name,s.login_account,s.amount_cents,s.before_cents,s.after_cents,s.settled_cents,s.payment_method,s.reference,s.remark,s.actor_id,s.created_at`
const settlementFrom = ` FROM supplier.supplier_settlements s JOIN supplier.supplier_finance_ledger l ON l.settlement_id=s.id `

func scanSettlement(row scanner) (ports.Settlement, error) {
	var v ports.Settlement
	var amount, before, after, settled int64
	e := row.Scan(&v.ID, &v.LedgerID, &v.SupplierID, &v.SupplierName, &v.Login, &amount, &before, &after, &settled, &v.Method, &v.Reference, &v.Remark, &v.ActorID, &v.CreatedAt)
	v.Amount = cents(amount)
	v.Before = cents(before)
	v.After = cents(after)
	v.Settled = cents(settled)
	return v, mapError(e)
}
func (s *Store) Settlement(ctx context.Context, id string) (ports.Settlement, error) {
	return scanSettlement(s.db.QueryRowContext(ctx, `SELECT `+settlementColumns+settlementFrom+` WHERE s.id=$1 AND s.upstream_instance_id=$2`, id, s.instanceID))
}
func (s *Store) Settle(ctx context.Context, in ports.SettlementCommand) (ports.Settlement, bool, error) {
	replay := false
	out, e := transaction(ctx, s.db, func(tx *sql.Tx) (ports.Settlement, error) {
		var zero ports.Settlement
		if in.ActorID <= 0 || in.Cents <= 0 || in.Key == "" || !validHash(in.Hash) {
			return zero, ports.ErrForbidden
		}
		if _, e := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("%s:finance:%d:%s", s.instanceID, in.ActorID, in.Key)); e != nil {
			return zero, e
		}
		var hash, id string
		e := tx.QueryRowContext(ctx, `SELECT id::text,encode(request_hash,'hex') FROM supplier.supplier_settlements WHERE upstream_instance_id=$1 AND actor_id=$2 AND idempotency_key=$3`, s.instanceID, in.ActorID, in.Key).Scan(&id, &hash)
		if e == nil {
			if hash != in.Hash {
				return zero, ports.ErrConflict
			}
			replay = true
			return scanSettlement(tx.QueryRowContext(ctx, `SELECT `+settlementColumns+settlementFrom+` WHERE s.id=$1`, id))
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return zero, e
		}
		var before, settled int64
		var name, login string
		e = tx.QueryRowContext(ctx, `SELECT b.unsettled_cents,b.settled_cents,p.name,COALESCE(p.login_name,'') FROM supplier.supplier_balances b JOIN supplier.suppliers p ON p.id=b.supplier_id WHERE b.supplier_id=$1 FOR UPDATE OF b`, in.SupplierID).Scan(&before, &settled, &name, &login)
		if e != nil {
			return zero, e
		}
		if in.Cents > before || settled > math.MaxInt64-in.Cents {
			return zero, ports.ErrConflict
		}
		id, e = newID()
		if e != nil {
			return zero, e
		}
		ledgerID, e := newID()
		if e != nil {
			return zero, e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO supplier.supplier_settlements(id,supplier_id,supplier_name,login_account,amount_cents,before_cents,after_cents,settled_cents,payment_method,reference,remark,actor_id,upstream_instance_id,idempotency_key,request_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,decode($15,'hex'))`, id, in.SupplierID, name, login, in.Cents, before, before-in.Cents, settled+in.Cents, in.Input.Method, in.Input.Reference, in.Input.Remark, in.ActorID, s.instanceID, in.Key, in.Hash)
		if e != nil {
			return zero, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO supplier.supplier_finance_ledger(id,settlement_id) VALUES($1,$2)`, ledgerID, id); e != nil {
			return zero, e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE supplier.supplier_balances SET unsettled_cents=$2,settled_cents=$3,version=version+1 WHERE supplier_id=$1`, in.SupplierID, before-in.Cents, settled+in.Cents); e != nil {
			return zero, e
		}
		auditID, e := newID()
		if e != nil {
			return zero, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO supplier.supplier_audit_events(id,supplier_id,actor_user_id,actor_kind,actor_id,resource_type,resource_id,action,result,request_id) VALUES($1,$2,$3,'admin',($3::bigint)::text,'settlement',$4,'supplier.settle','succeeded',$4)`, auditID, in.SupplierID, in.ActorID, id); e != nil {
			return zero, e
		}
		return scanSettlement(tx.QueryRowContext(ctx, `SELECT `+settlementColumns+settlementFrom+` WHERE s.id=$1`, id))
	})
	return out, replay, e
}
func (s *Store) Finance(ctx context.Context, q ports.FinanceQuery) (ports.FinanceReport, error) {
	out := ports.FinanceReport{Items: []ports.Settlement{}, Page: q.Page, Size: q.Size, Overview: ports.FinanceOverview{Accrual: "official_usd_v1"}}
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	// Numeric aggregation avoids int64 overflow across suppliers; text keeps exact cents.
	var unsettled, settled string
	e = tx.QueryRowContext(ctx, `SELECT (COALESCE(sum(unsettled_cents),0)/100)::numeric(30,2)::text,(COALESCE(sum(settled_cents),0)/100)::numeric(30,2)::text FROM supplier.supplier_balances WHERE ($1='' OR supplier_id::text=$1)`, q.SupplierID).Scan(&unsettled, &settled)
	if e != nil {
		return out, e
	}
	out.Overview.Unsettled = unsettled
	out.Overview.Settled = settled
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM supplier.supplier_settlements WHERE upstream_instance_id=$2 AND ($1='' OR supplier_id::text=$1)`, q.SupplierID, s.instanceID).Scan(&out.Overview.Count); e != nil {
		return out, e
	}
	const where = ` WHERE s.upstream_instance_id=$1 AND ($2='' OR s.supplier_id::text=$2) AND ($3='' OR strpos(lower(concat_ws(' ',s.supplier_name,s.login_account,s.id::text,l.id::text,s.reference)),lower($3))>0)`
	if e = tx.QueryRowContext(ctx, `SELECT count(*)`+settlementFrom+where, s.instanceID, q.SupplierID, q.Search).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT `+settlementColumns+settlementFrom+where+` ORDER BY s.created_at DESC,s.id DESC LIMIT $4 OFFSET $5`, s.instanceID, q.SupplierID, q.Search, q.Size, (q.Page-1)*q.Size)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		v, e := scanSettlement(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		out.Items = append(out.Items, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	out.Pages = (out.Total + int64(q.Size) - 1) / int64(q.Size)
	return out, tx.Commit()
}
