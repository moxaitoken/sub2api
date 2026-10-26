package repository

import (
	"context"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"time"
)

func (r *usageLogRepository) GetAccountRuntimeTokens(ctx context.Context, ids []int64, start, end time.Time) (map[int64]service.AccountRuntimeTokens, error) {
	if len(ids) > 100 || !end.After(start) || end.Sub(start) > time.Minute {
		return nil, fmt.Errorf("invalid runtime window")
	}
	result := make(map[int64]service.AccountRuntimeTokens, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, fmt.Errorf("invalid runtime account")
		}
		result[id] = service.AccountRuntimeTokens{Tokens: "0", UncachedTokens: "0"}
	}
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := r.sql.QueryContext(ctx, `SELECT account_id,
 COALESCE(sum(input_tokens::numeric+output_tokens+cache_creation_tokens+cache_read_tokens),0)::text,
 COALESCE(sum(input_tokens::numeric+output_tokens),0)::text
 FROM usage_logs WHERE account_id=ANY($1) AND created_at >= $2 AND created_at < $3 GROUP BY account_id`, pq.Array(ids), start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var value service.AccountRuntimeTokens
		if err = rows.Scan(&id, &value.Tokens, &value.UncachedTokens); err != nil {
			return nil, err
		}
		result[id] = value
	}
	return result, rows.Err()
}
