package supplier

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func newMockRepo(t *testing.T) (*sqlRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return &sqlRepository{db: db}, mock
}

func TestAggregateUsageScopesAccountsAndUsesAccountCost(t *testing.T) {
	repo, mock := newMockRepo(t)
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)

	mock.ExpectQuery(regexp.QuoteMeta(`COALESCE(SUM(COALESCE(ul.account_stats_cost, ul.total_cost) * COALESCE(ul.account_rate_multiplier, 1)), 0)`)).
		WithArgs(pq.Array([]int64{3, 4}), start, end, int64(4), "claude-opus-5-5").
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "name", "model", "n", "in", "out", "cc", "cr", "amount"}).
			AddRow(int64(4), "acc-4", "claude-opus-5-5", int64(2), int64(100), int64(50), int64(10), int64(20), 1.25))

	rows, err := repo.AggregateUsage(context.Background(), UsageQuery{
		AccountIDs: []int64{3, 4}, Start: start, End: end, AccountID: 4, Model: "claude-opus-5-5",
	}, GroupByAccountModel)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, UsageRow{
		AccountID: 4, AccountName: "acc-4", Model: "claude-opus-5-5", Family: FamilyOpus,
		UsageTotals: UsageTotals{RequestCount: 2, InputTokens: 100, OutputTokens: 50,
			CacheCreationTokens: 10, CacheReadTokens: 20, Amount: 1.25},
	}, rows[0])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAggregateUsageWithoutAccountsSkipsQuery(t *testing.T) {
	repo, mock := newMockRepo(t)
	rows, err := repo.AggregateUsage(context.Background(), UsageQuery{Start: time.Now(), End: time.Now()}, GroupByModel)
	require.NoError(t, err)
	require.Empty(t, rows)
	require.NoError(t, mock.ExpectationsWereMet(), "an empty account set must not query all usage")
}

func TestCreateSettlementRejectsOverlap(t *testing.T) {
	repo, mock := newMockRepo(t)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`SELECT id FROM suppliers WHERE id = $1 FOR UPDATE`)).
		WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(`status <> 'void'`)).
		WithArgs(int64(1), start, end).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectRollback()

	err := repo.CreateSettlement(context.Background(), &Settlement{SupplierID: 1, PeriodStart: start, PeriodEnd: end})
	require.ErrorIs(t, err, ErrSettlementOverlap)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTransitionSettlementDistinguishesStatusFromMissing(t *testing.T) {
	repo, mock := newMockRepo(t)
	update := regexp.QuoteMeta(`UPDATE supplier_settlements SET status = $2, updated_at = NOW(), paid_at = NOW() WHERE id = $1 AND status = ANY($3)`)
	exists := regexp.QuoteMeta(`SELECT EXISTS (SELECT 1 FROM supplier_settlements WHERE id = $1)`)

	mock.ExpectExec(update).WithArgs(int64(5), SettlementPaid, pq.Array([]string{SettlementConfirmed})).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(exists).WithArgs(int64(5)).WillReturnRows(sqlmock.NewRows([]string{"e"}).AddRow(true))
	err := repo.TransitionSettlement(context.Background(), 5, []string{SettlementConfirmed}, SettlementPaid)
	require.ErrorIs(t, err, ErrSettlementStatus)

	mock.ExpectExec(update).WithArgs(int64(6), SettlementPaid, pq.Array([]string{SettlementConfirmed})).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(exists).WithArgs(int64(6)).WillReturnRows(sqlmock.NewRows([]string{"e"}).AddRow(false))
	err = repo.TransitionSettlement(context.Background(), 6, []string{SettlementConfirmed}, SettlementPaid)
	require.ErrorIs(t, err, ErrSettlementNotFound)

	mock.ExpectExec(update).WithArgs(int64(7), SettlementPaid, pq.Array([]string{SettlementConfirmed})).
		WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, repo.TransitionSettlement(context.Background(), 7, []string{SettlementConfirmed}, SettlementPaid))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateSupplierMapsConstraintErrors(t *testing.T) {
	repo, mock := newMockRepo(t)
	insert := regexp.QuoteMeta(`INSERT INTO suppliers`)

	mock.ExpectQuery(insert).WillReturnError(&pq.Error{Code: "23505"})
	require.ErrorIs(t, repo.CreateSupplier(context.Background(), &Supplier{UserID: 1, Name: "a"}), ErrSupplierExists)

	mock.ExpectQuery(insert).WillReturnError(&pq.Error{Code: "23503"})
	require.ErrorIs(t, repo.CreateSupplier(context.Background(), &Supplier{UserID: 404, Name: "a"}), ErrUserNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}
