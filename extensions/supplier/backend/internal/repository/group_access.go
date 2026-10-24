package repository

import (
	"context"
	"database/sql"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

func (s *Store) GroupAccess(ctx context.Context, id int64) (ports.GroupAccess, error) {
	x := ports.GroupAccess{GroupID: id}
	err := s.db.QueryRowContext(ctx, `SELECT enabled,version,supplier_group_name FROM supplier.group_access WHERE upstream_instance_id=$1 AND group_id=$2`, s.instanceID, id).Scan(&x.Enabled, &x.Version, &x.SupplierGroupName)
	if err == sql.ErrNoRows {
		return x, nil
	}
	return x, mapError(err)
}
func (s *Store) ListGroupAccess(ctx context.Context) ([]ports.GroupAccess, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT group_id,enabled,version,supplier_group_name FROM supplier.group_access WHERE upstream_instance_id=$1 ORDER BY group_id`, s.instanceID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ports.GroupAccess{}
	for rows.Next() {
		var x ports.GroupAccess
		if e = rows.Scan(&x.GroupID, &x.Enabled, &x.Version, &x.SupplierGroupName); e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Store) EnabledGroupAccess(ctx context.Context) ([]ports.GroupAccess, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT group_id,enabled,version,supplier_group_name FROM supplier.group_access WHERE upstream_instance_id=$1 AND enabled ORDER BY group_id`, s.instanceID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ports.GroupAccess{}
	for rows.Next() {
		var x ports.GroupAccess
		if e = rows.Scan(&x.GroupID, &x.Enabled, &x.Version, &x.SupplierGroupName); e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Store) EnabledGroupIDs(ctx context.Context) ([]int64, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT group_id FROM supplier.group_access WHERE upstream_instance_id=$1 AND enabled ORDER BY group_id`, s.instanceID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (s *Store) SetGroupAccess(ctx context.Context, in ports.GroupAccess, actor int64) (ports.GroupAccess, error) {
	if actor <= 0 || in.GroupID <= 0 || in.Version < 0 {
		return in, ports.ErrForbidden
	}
	return transaction(ctx, s.db, func(tx *sql.Tx) (ports.GroupAccess, error) {
		var version int64
		e := tx.QueryRowContext(ctx, `INSERT INTO supplier.group_access(upstream_instance_id,group_id,enabled,supplier_group_name) SELECT $1,$2,$3,$5 WHERE $4::bigint=0 ON CONFLICT(upstream_instance_id,group_id) DO NOTHING RETURNING version`, s.instanceID, in.GroupID, in.Enabled, in.Version, in.SupplierGroupName).Scan(&version)
		if e == sql.ErrNoRows {
			e = tx.QueryRowContext(ctx, `UPDATE supplier.group_access SET enabled=$3,supplier_group_name=$5,version=version+1,updated_at=now() WHERE upstream_instance_id=$1 AND group_id=$2 AND version=$4 RETURNING version`, s.instanceID, in.GroupID, in.Enabled, in.Version, in.SupplierGroupName).Scan(&version)
		}
		if e == sql.ErrNoRows {
			return in, ports.ErrConflict
		}
		if e != nil {
			return in, e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO supplier.group_access_audit(upstream_instance_id,group_id,enabled,supplier_group_name,actor_id,version) VALUES($1,$2,$3,$4,$5,$6)`, s.instanceID, in.GroupID, in.Enabled, in.SupplierGroupName, actor, version)
		in.Version = version
		return in, e
	})
}

// Seed once only: later restarts must not re-enable switches disabled by an admin.
func (s *Store) SeedGroupAccess(ctx context.Context, ids []int64) error {
	_, e := transaction(ctx, s.db, func(tx *sql.Tx) (bool, error) {
		res, e := tx.ExecContext(ctx, `INSERT INTO supplier.group_access_initializations VALUES($1) ON CONFLICT DO NOTHING`, s.instanceID)
		if e != nil {
			return false, e
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return false, nil
		}
		for _, id := range ids {
			if id <= 0 {
				return false, ports.ErrConflict
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO supplier.group_access(upstream_instance_id,group_id,enabled) VALUES($1,$2,true) ON CONFLICT DO NOTHING`, s.instanceID, id); e != nil {
				return false, e
			}
		}
		return true, nil
	})
	return e
}
