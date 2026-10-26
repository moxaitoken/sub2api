package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestAccountRuntimeTokensExactBoundsAndErrors(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	r := &usageLogRepository{sql: db}
	end := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	start := end.Add(-time.Minute)
	query := regexp.QuoteMeta(`SELECT account_id, COALESCE(sum(input_tokens::numeric+output_tokens+cache_creation_tokens+cache_read_tokens),0)::text, COALESCE(sum(input_tokens::numeric+output_tokens),0)::text FROM usage_logs WHERE account_id=ANY($1) AND created_at >= $2 AND created_at < $3 GROUP BY account_id`)
	mock.ExpectQuery(query).WithArgs(sqlmock.AnyArg(), start, end).WillReturnRows(sqlmock.NewRows([]string{"account_id", "tokens", "uncached"}).AddRow(1, "9007199254740993", "100"))
	out, err := r.GetAccountRuntimeTokens(context.Background(), []int64{1, 2}, start, end)
	require.NoError(t, err)
	require.Equal(t, "9007199254740993", out[1].Tokens)
	require.Equal(t, "100", out[1].UncachedTokens)
	require.Equal(t, "0", out[2].Tokens)
	mock.ExpectQuery(query).WillReturnError(errors.New("fixture DB failure"))
	_, err = r.GetAccountRuntimeTokens(context.Background(), []int64{1}, start, end)
	require.Error(t, err)
	_, err = r.GetAccountRuntimeTokens(context.Background(), []int64{1}, start.Add(-time.Second), end)
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
func TestAccountRuntimeRPMNaturalMinuteZeroAndMalformed(t *testing.T) {
	s := miniredis.RunT(t)
	s.SetTime(time.Date(2026, 10, 24, 12, 0, 30, 0, time.UTC))
	rdb := redis.NewClient(&redis.Options{Addr: s.Addr(), MaxRetries: -1})
	defer rdb.Close()
	c := &RPMCacheImpl{rdb: rdb}
	ctx := context.Background()
	_, err := c.IncrementRPM(ctx, 1)
	require.NoError(t, err)
	out, err := c.GetRuntimeRPM(ctx, []int64{1, 2})
	require.NoError(t, err)
	require.Equal(t, 1, out[1])
	require.Equal(t, 0, out[2])
	s.SetTime(time.Date(2026, 10, 24, 12, 1, 0, 0, time.UTC))
	out, err = c.GetRuntimeRPM(ctx, []int64{1})
	require.NoError(t, err)
	require.Equal(t, 0, out[1])
	key, err := c.currentMinuteKey(ctx, 1)
	require.NoError(t, err)
	s.Set(key, "not-a-counter")
	_, err = c.GetRuntimeRPM(ctx, []int64{1})
	require.Error(t, err)
	rdb.Close()
	_, err = c.GetRuntimeRPM(ctx, []int64{1})
	require.Error(t, err)
}
