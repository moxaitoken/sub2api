package supplier

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/lib/pq"
)

// UsageGrouping selects the buckets AggregateUsage returns.
type UsageGrouping int

const (
	GroupByModel UsageGrouping = iota
	GroupByDateModel
	GroupByAccount
	GroupByAccountModel
)

// Repository is the persistence boundary of the supplier module.
type Repository interface {
	GetSupplier(ctx context.Context, id int64) (*Supplier, error)
	GetSupplierByUserID(ctx context.Context, userID int64) (*Supplier, error)
	FindUserIDByEmail(ctx context.Context, email string) (int64, error)
	ListSuppliers(ctx context.Context, params pagination.PaginationParams, search string) ([]Supplier, int64, error)
	CreateSupplier(ctx context.Context, s *Supplier) error
	UpdateSupplier(ctx context.Context, s *Supplier) error

	LinkAccount(ctx context.Context, link *AccountLink) error
	GetAccountLink(ctx context.Context, accountID int64) (*AccountLink, error)
	// ListAccountLinks returns links of accounts that are not soft-deleted. supplierID 0
	// means all suppliers; an empty reviewStatus means any status.
	ListAccountLinks(ctx context.Context, supplierID int64, reviewStatus string) ([]AccountLink, error)
	CountActiveAccounts(ctx context.Context, supplierID int64) (int64, error)
	// AllAccountIDs includes soft-deleted accounts so their historical usage stays attributed.
	AllAccountIDs(ctx context.Context, supplierID int64) ([]int64, error)
	UpdateAccountReview(ctx context.Context, accountID int64, status, note string, reviewerID int64) error

	LinkProxy(ctx context.Context, proxyID, supplierID int64) error
	GetProxyOwner(ctx context.Context, proxyID int64) (int64, error)
	ListProxyIDs(ctx context.Context, supplierID int64) ([]int64, error)
	CountAccountsByProxy(ctx context.Context, proxyIDs []int64) (map[int64]int64, error)

	AggregateUsage(ctx context.Context, q UsageQuery, grouping UsageGrouping) ([]UsageRow, error)

	CreateSettlement(ctx context.Context, s *Settlement) error
	GetSettlement(ctx context.Context, id int64) (*Settlement, error)
	ListSettlements(ctx context.Context, supplierID int64, status string, params pagination.PaginationParams) ([]Settlement, int64, error)
	// TransitionSettlement moves a settlement to `to` only if it is currently in one of `from`.
	TransitionSettlement(ctx context.Context, id int64, from []string, to string) error
}

type sqlRepository struct {
	db *sql.DB
}

// NewRepository builds the SQL repository on the shared database handle.
func NewRepository(db *sql.DB) Repository {
	return &sqlRepository{db: db}
}

const supplierColumns = `s.id, s.user_id, COALESCE(u.email, ''), s.name, s.status, s.default_concurrency,
	s.max_accounts, s.notes, s.created_at, s.updated_at,
	(SELECT COUNT(*) FROM supplier_accounts sa JOIN accounts a ON a.id = sa.account_id
	  WHERE sa.supplier_id = s.id AND a.deleted_at IS NULL),
	(SELECT COUNT(*) FROM supplier_proxies sp JOIN proxies p ON p.id = sp.proxy_id
	  WHERE sp.supplier_id = s.id AND p.deleted_at IS NULL)`

func scanSupplier(row interface{ Scan(...any) error }) (*Supplier, error) {
	var s Supplier
	if err := row.Scan(&s.ID, &s.UserID, &s.UserEmail, &s.Name, &s.Status, &s.DefaultConcurrency,
		&s.MaxAccounts, &s.Notes, &s.CreatedAt, &s.UpdatedAt, &s.AccountCount, &s.ProxyCount); err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *sqlRepository) getSupplierWhere(ctx context.Context, where string, arg any) (*Supplier, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+supplierColumns+` FROM suppliers s LEFT JOIN users u ON u.id = s.user_id WHERE `+where, arg)
	s, err := scanSupplier(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSupplierNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get supplier: %w", err)
	}
	return s, nil
}

func (r *sqlRepository) GetSupplier(ctx context.Context, id int64) (*Supplier, error) {
	return r.getSupplierWhere(ctx, "s.id = $1", id)
}

func (r *sqlRepository) GetSupplierByUserID(ctx context.Context, userID int64) (*Supplier, error) {
	return r.getSupplierWhere(ctx, "s.user_id = $1", userID)
}

func (r *sqlRepository) FindUserIDByEmail(ctx context.Context, email string) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx,
		`SELECT id FROM users WHERE LOWER(email) = LOWER($1) AND deleted_at IS NULL`, strings.TrimSpace(email)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrUserNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("find user by email: %w", err)
	}
	return id, nil
}

func (r *sqlRepository) ListSuppliers(ctx context.Context, params pagination.PaginationParams, search string) ([]Supplier, int64, error) {
	where := "TRUE"
	args := []any{}
	if search = strings.TrimSpace(search); search != "" {
		args = append(args, "%"+escapeLike(search)+"%")
		where = "(s.name ILIKE $1 OR u.email ILIKE $1)"
	}

	var total int64
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM suppliers s LEFT JOIN users u ON u.id = s.user_id WHERE `+where, args...,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count suppliers: %w", err)
	}

	limit, offset := pageBounds(params)
	args = append(args, limit, offset)
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+supplierColumns+` FROM suppliers s LEFT JOIN users u ON u.id = s.user_id WHERE `+where+
			fmt.Sprintf(" ORDER BY s.id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list suppliers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []Supplier{}
	for rows.Next() {
		s, err := scanSupplier(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan supplier: %w", err)
		}
		out = append(out, *s)
	}
	return out, total, rows.Err()
}

func (r *sqlRepository) CreateSupplier(ctx context.Context, s *Supplier) error {
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO suppliers (user_id, name, status, default_concurrency, max_accounts, notes)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at, updated_at`,
		s.UserID, s.Name, s.Status, s.DefaultConcurrency, s.MaxAccounts, s.Notes,
	).Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt)
	if isUniqueViolation(err) {
		return ErrSupplierExists
	}
	if isForeignKeyViolation(err) {
		return ErrUserNotFound
	}
	if err != nil {
		return fmt.Errorf("insert supplier: %w", err)
	}
	return nil
}

func (r *sqlRepository) UpdateSupplier(ctx context.Context, s *Supplier) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE suppliers SET name = $2, status = $3, default_concurrency = $4, max_accounts = $5,
		 notes = $6, updated_at = NOW() WHERE id = $1`,
		s.ID, s.Name, s.Status, s.DefaultConcurrency, s.MaxAccounts, s.Notes)
	if err != nil {
		return fmt.Errorf("update supplier: %w", err)
	}
	return requireAffected(res, ErrSupplierNotFound)
}

func (r *sqlRepository) LinkAccount(ctx context.Context, link *AccountLink) error {
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO supplier_accounts (account_id, supplier_id, review_status) VALUES ($1, $2, $3)
		 RETURNING created_at`,
		link.AccountID, link.SupplierID, link.ReviewStatus,
	).Scan(&link.CreatedAt)
	if isUniqueViolation(err) {
		return ErrAccountAlreadyLinked
	}
	if err != nil {
		return fmt.Errorf("link account: %w", err)
	}
	return nil
}

const accountLinkColumns = `sa.account_id, sa.supplier_id, sa.review_status, sa.review_note,
	sa.reviewed_by, sa.reviewed_at, sa.created_at`

func scanAccountLink(row interface{ Scan(...any) error }) (*AccountLink, error) {
	var link AccountLink
	var reviewedBy sql.NullInt64
	var reviewedAt sql.NullTime
	if err := row.Scan(&link.AccountID, &link.SupplierID, &link.ReviewStatus, &link.ReviewNote,
		&reviewedBy, &reviewedAt, &link.CreatedAt); err != nil {
		return nil, err
	}
	if reviewedBy.Valid {
		link.ReviewedBy = &reviewedBy.Int64
	}
	if reviewedAt.Valid {
		link.ReviewedAt = &reviewedAt.Time
	}
	return &link, nil
}

func (r *sqlRepository) GetAccountLink(ctx context.Context, accountID int64) (*AccountLink, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+accountLinkColumns+` FROM supplier_accounts sa WHERE sa.account_id = $1`, accountID)
	link, err := scanAccountLink(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAccountNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get account link: %w", err)
	}
	return link, nil
}

func (r *sqlRepository) ListAccountLinks(ctx context.Context, supplierID int64, reviewStatus string) ([]AccountLink, error) {
	query := `SELECT ` + accountLinkColumns + ` FROM supplier_accounts sa
		JOIN accounts a ON a.id = sa.account_id WHERE a.deleted_at IS NULL`
	args := []any{}
	if supplierID > 0 {
		args = append(args, supplierID)
		query += fmt.Sprintf(" AND sa.supplier_id = $%d", len(args))
	}
	if reviewStatus != "" {
		args = append(args, reviewStatus)
		query += fmt.Sprintf(" AND sa.review_status = $%d", len(args))
	}
	query += " ORDER BY sa.created_at DESC, sa.account_id DESC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list account links: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []AccountLink{}
	for rows.Next() {
		link, err := scanAccountLink(rows)
		if err != nil {
			return nil, fmt.Errorf("scan account link: %w", err)
		}
		out = append(out, *link)
	}
	return out, rows.Err()
}

func (r *sqlRepository) CountActiveAccounts(ctx context.Context, supplierID int64) (int64, error) {
	var n int64
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM supplier_accounts sa JOIN accounts a ON a.id = sa.account_id
		 WHERE sa.supplier_id = $1 AND a.deleted_at IS NULL`, supplierID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count supplier accounts: %w", err)
	}
	return n, nil
}

func (r *sqlRepository) AllAccountIDs(ctx context.Context, supplierID int64) ([]int64, error) {
	return r.queryIDs(ctx, `SELECT account_id FROM supplier_accounts WHERE supplier_id = $1 ORDER BY account_id`, supplierID)
}

func (r *sqlRepository) UpdateAccountReview(ctx context.Context, accountID int64, status, note string, reviewerID int64) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE supplier_accounts SET review_status = $2, review_note = $3, reviewed_by = $4, reviewed_at = NOW()
		 WHERE account_id = $1`, accountID, status, note, reviewerID)
	if err != nil {
		return fmt.Errorf("update account review: %w", err)
	}
	return requireAffected(res, ErrAccountNotFound)
}

func (r *sqlRepository) LinkProxy(ctx context.Context, proxyID, supplierID int64) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO supplier_proxies (proxy_id, supplier_id) VALUES ($1, $2)`, proxyID, supplierID)
	if isUniqueViolation(err) {
		return ErrProxyAlreadyLinked
	}
	if err != nil {
		return fmt.Errorf("link proxy: %w", err)
	}
	return nil
}

func (r *sqlRepository) GetProxyOwner(ctx context.Context, proxyID int64) (int64, error) {
	var supplierID int64
	err := r.db.QueryRowContext(ctx,
		`SELECT sp.supplier_id FROM supplier_proxies sp JOIN proxies p ON p.id = sp.proxy_id
		 WHERE sp.proxy_id = $1 AND p.deleted_at IS NULL`, proxyID).Scan(&supplierID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrProxyNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("get proxy owner: %w", err)
	}
	return supplierID, nil
}

func (r *sqlRepository) ListProxyIDs(ctx context.Context, supplierID int64) ([]int64, error) {
	return r.queryIDs(ctx,
		`SELECT sp.proxy_id FROM supplier_proxies sp JOIN proxies p ON p.id = sp.proxy_id
		 WHERE sp.supplier_id = $1 AND p.deleted_at IS NULL ORDER BY sp.proxy_id DESC`, supplierID)
}

func (r *sqlRepository) CountAccountsByProxy(ctx context.Context, proxyIDs []int64) (map[int64]int64, error) {
	out := make(map[int64]int64, len(proxyIDs))
	if len(proxyIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT proxy_id, COUNT(*) FROM accounts
		 WHERE proxy_id = ANY($1) AND deleted_at IS NULL GROUP BY proxy_id`, pq.Array(proxyIDs))
	if err != nil {
		return nil, fmt.Errorf("count accounts by proxy: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, n int64
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// effectiveModelExpr matches the upstream effective-upstream-model index (migration 226).
const effectiveModelExpr = `COALESCE(NULLIF(BTRIM(ul.upstream_model), ''), ul.model)`

const usageAggregates = `COUNT(*),
	COALESCE(SUM(ul.input_tokens), 0),
	COALESCE(SUM(ul.output_tokens), 0),
	COALESCE(SUM(ul.cache_creation_tokens), 0),
	COALESCE(SUM(ul.cache_read_tokens), 0),
	COALESCE(SUM(COALESCE(ul.account_stats_cost, ul.total_cost) * COALESCE(ul.account_rate_multiplier, 1)), 0)`

func (r *sqlRepository) AggregateUsage(ctx context.Context, q UsageQuery, grouping UsageGrouping) ([]UsageRow, error) {
	if len(q.AccountIDs) == 0 {
		return []UsageRow{}, nil
	}

	args := []any{pq.Array(q.AccountIDs), q.Start, q.End}
	where := `ul.account_id = ANY($1) AND ul.created_at >= $2 AND ul.created_at < $3`
	if q.AccountID > 0 {
		args = append(args, q.AccountID)
		where += fmt.Sprintf(" AND ul.account_id = $%d", len(args))
	}
	if q.Model != "" {
		args = append(args, q.Model)
		where += fmt.Sprintf(" AND %s = $%d", effectiveModelExpr, len(args))
	}

	var selectCols, groupCols, orderBy string
	switch grouping {
	case GroupByModel:
		selectCols = effectiveModelExpr
		groupCols = "1"
		orderBy = "1"
	case GroupByDateModel:
		args = append(args, resolveTimezone())
		selectCols = fmt.Sprintf("TO_CHAR(ul.created_at AT TIME ZONE $%d, 'YYYY-MM-DD'), %s", len(args), effectiveModelExpr)
		groupCols = "1, 2"
		orderBy = "1, 2"
	case GroupByAccount:
		selectCols = "ul.account_id, COALESCE(MAX(a.name), '')"
		groupCols = "ul.account_id"
		orderBy = "ul.account_id"
	case GroupByAccountModel:
		selectCols = fmt.Sprintf("ul.account_id, COALESCE(MAX(a.name), ''), %s", effectiveModelExpr)
		groupCols = "ul.account_id, 3"
		orderBy = "ul.account_id, 3"
	default:
		return nil, fmt.Errorf("unknown usage grouping %d", grouping)
	}

	query := `SELECT ` + selectCols + `, ` + usageAggregates + `
		FROM usage_logs ul LEFT JOIN accounts a ON a.id = ul.account_id
		WHERE ` + where + ` GROUP BY ` + groupCols + ` ORDER BY ` + orderBy

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("aggregate supplier usage: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []UsageRow{}
	for rows.Next() {
		var row UsageRow
		totals := []any{&row.RequestCount, &row.InputTokens, &row.OutputTokens,
			&row.CacheCreationTokens, &row.CacheReadTokens, &row.Amount}
		var dest []any
		switch grouping {
		case GroupByModel:
			dest = append([]any{&row.Model}, totals...)
		case GroupByDateModel:
			dest = append([]any{&row.Date, &row.Model}, totals...)
		case GroupByAccount:
			dest = append([]any{&row.AccountID, &row.AccountName}, totals...)
		case GroupByAccountModel:
			dest = append([]any{&row.AccountID, &row.AccountName, &row.Model}, totals...)
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("scan supplier usage: %w", err)
		}
		if row.Model != "" {
			row.Family = ModelFamily(row.Model)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *sqlRepository) CreateSettlement(ctx context.Context, s *Settlement) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Serialize settlement creation per supplier so the overlap check below is reliable.
	if _, err := tx.ExecContext(ctx, `SELECT id FROM suppliers WHERE id = $1 FOR UPDATE`, s.SupplierID); err != nil {
		return fmt.Errorf("lock supplier: %w", err)
	}
	var overlapping bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM supplier_settlements WHERE supplier_id = $1 AND status <> 'void'
		 AND period_start < $3 AND period_end > $2)`,
		s.SupplierID, s.PeriodStart, s.PeriodEnd,
	).Scan(&overlapping); err != nil {
		return fmt.Errorf("check settlement overlap: %w", err)
	}
	if overlapping {
		return ErrSettlementOverlap
	}

	if err := tx.QueryRowContext(ctx,
		`INSERT INTO supplier_settlements (supplier_id, period_start, period_end, status, request_count,
		 input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens, total_amount, notes, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) RETURNING id, created_at, updated_at`,
		s.SupplierID, s.PeriodStart, s.PeriodEnd, s.Status, s.RequestCount, s.InputTokens, s.OutputTokens,
		s.CacheCreationTokens, s.CacheReadTokens, s.Amount, s.Notes, s.CreatedBy,
	).Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return fmt.Errorf("insert settlement: %w", err)
	}

	for _, item := range s.Items {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO supplier_settlement_items (settlement_id, account_id, account_name, model, request_count,
			 input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens, amount)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			s.ID, item.AccountID, item.AccountName, item.Model, item.RequestCount, item.InputTokens,
			item.OutputTokens, item.CacheCreationTokens, item.CacheReadTokens, item.Amount,
		); err != nil {
			return fmt.Errorf("insert settlement item: %w", err)
		}
	}
	return tx.Commit()
}

const settlementColumns = `st.id, st.supplier_id, COALESCE(s.name, ''), st.period_start, st.period_end, st.status,
	st.request_count, st.input_tokens, st.output_tokens, st.cache_creation_tokens, st.cache_read_tokens,
	st.total_amount, st.notes, st.created_by, st.confirmed_at, st.paid_at, st.voided_at, st.created_at, st.updated_at`

func scanSettlement(row interface{ Scan(...any) error }) (*Settlement, error) {
	var st Settlement
	var createdBy sql.NullInt64
	var confirmedAt, paidAt, voidedAt sql.NullTime
	if err := row.Scan(&st.ID, &st.SupplierID, &st.SupplierName, &st.PeriodStart, &st.PeriodEnd, &st.Status,
		&st.RequestCount, &st.InputTokens, &st.OutputTokens, &st.CacheCreationTokens, &st.CacheReadTokens,
		&st.Amount, &st.Notes, &createdBy, &confirmedAt, &paidAt, &voidedAt, &st.CreatedAt, &st.UpdatedAt); err != nil {
		return nil, err
	}
	if createdBy.Valid {
		st.CreatedBy = &createdBy.Int64
	}
	st.ConfirmedAt = nullTimePtr(confirmedAt)
	st.PaidAt = nullTimePtr(paidAt)
	st.VoidedAt = nullTimePtr(voidedAt)
	return &st, nil
}

func (r *sqlRepository) GetSettlement(ctx context.Context, id int64) (*Settlement, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+settlementColumns+` FROM supplier_settlements st
		 LEFT JOIN suppliers s ON s.id = st.supplier_id WHERE st.id = $1`, id)
	st, err := scanSettlement(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSettlementNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get settlement: %w", err)
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT account_id, account_name, model, request_count, input_tokens, output_tokens,
		 cache_creation_tokens, cache_read_tokens, amount
		 FROM supplier_settlement_items WHERE settlement_id = $1 ORDER BY account_id, model`, id)
	if err != nil {
		return nil, fmt.Errorf("list settlement items: %w", err)
	}
	defer func() { _ = rows.Close() }()
	st.Items = []SettlementItem{}
	for rows.Next() {
		var item SettlementItem
		if err := rows.Scan(&item.AccountID, &item.AccountName, &item.Model, &item.RequestCount,
			&item.InputTokens, &item.OutputTokens, &item.CacheCreationTokens, &item.CacheReadTokens,
			&item.Amount); err != nil {
			return nil, fmt.Errorf("scan settlement item: %w", err)
		}
		item.Family = ModelFamily(item.Model)
		st.Items = append(st.Items, item)
	}
	return st, rows.Err()
}

func (r *sqlRepository) ListSettlements(ctx context.Context, supplierID int64, status string, params pagination.PaginationParams) ([]Settlement, int64, error) {
	where := "TRUE"
	args := []any{}
	if supplierID > 0 {
		args = append(args, supplierID)
		where += fmt.Sprintf(" AND st.supplier_id = $%d", len(args))
	}
	if status != "" {
		args = append(args, status)
		where += fmt.Sprintf(" AND st.status = $%d", len(args))
	}

	var total int64
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM supplier_settlements st WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count settlements: %w", err)
	}

	limit, offset := pageBounds(params)
	args = append(args, limit, offset)
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+settlementColumns+` FROM supplier_settlements st
		 LEFT JOIN suppliers s ON s.id = st.supplier_id WHERE `+where+
			fmt.Sprintf(" ORDER BY st.period_start DESC, st.id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)),
		args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list settlements: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []Settlement{}
	for rows.Next() {
		st, err := scanSettlement(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan settlement: %w", err)
		}
		out = append(out, *st)
	}
	return out, total, rows.Err()
}

func (r *sqlRepository) TransitionSettlement(ctx context.Context, id int64, from []string, to string) error {
	stampColumn := map[string]string{
		SettlementConfirmed: "confirmed_at",
		SettlementPaid:      "paid_at",
		SettlementVoid:      "voided_at",
	}[to]
	set := "status = $2, updated_at = NOW()"
	if stampColumn != "" {
		set += ", " + stampColumn + " = NOW()"
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE supplier_settlements SET `+set+` WHERE id = $1 AND status = ANY($3)`,
		id, to, pq.Array(from))
	if err != nil {
		return fmt.Errorf("transition settlement: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	var exists bool
	if err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM supplier_settlements WHERE id = $1)`, id).Scan(&exists); err != nil {
		return fmt.Errorf("check settlement: %w", err)
	}
	if !exists {
		return ErrSettlementNotFound
	}
	return ErrSettlementStatus
}

func (r *sqlRepository) queryIDs(ctx context.Context, query string, args ...any) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// resolveTimezone mirrors the upstream usage-stats timezone resolution so daily buckets
// line up with the admin dashboard.
func resolveTimezone() string {
	if name := timezone.Name(); name != "" && name != "Local" {
		return name
	}
	if envTZ := strings.TrimSpace(os.Getenv("TZ")); envTZ != "" {
		return envTZ
	}
	return "UTC"
}

func pageBounds(params pagination.PaginationParams) (limit, offset int) {
	limit = params.PageSize
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	page := params.Page
	if page < 1 {
		page = 1
	}
	return limit, (page - 1) * limit
}

func requireAffected(res sql.Result, notFound error) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return notFound
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

func isForeignKeyViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23503"
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func nullTimePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}
