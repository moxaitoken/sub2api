//go:build integration

package repository

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"testing"
	"time"
)

func TestAccountRuntimePostgresBoundsAndPrecision(t *testing.T) {
	dsn := os.Getenv("SUPPLIER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("explicit isolated fixture database required")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Hostname() != "127.0.0.1" || u.Path != "/supplier_repository_test" {
		t.Fatal("refusing non-fixture database")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	// Session-local table cannot touch any existing application schema.
	if _, err = db.Exec(`CREATE TEMP TABLE usage_logs(account_id bigint,created_at timestamptz,input_tokens bigint,output_tokens bigint,cache_creation_tokens bigint,cache_read_tokens bigint)`); err != nil {
		t.Fatal(err)
	}
	end := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	start := end.Add(-time.Minute)
	for _, v := range []struct {
		id     int64
		at     time.Time
		tokens int64
	}{{1, start.Add(-time.Second), 999}, {1, start, 9007199254740993}, {1, end.Add(-time.Second), 7}, {1, end, 999}, {99, start, 999}} {
		if _, err = db.Exec(`INSERT INTO usage_logs VALUES($1,$2,$3,1,2,3)`, v.id, v.at, v.tokens); err != nil {
			t.Fatal(err)
		}
	}
	r := &usageLogRepository{sql: db}
	values, err := r.GetAccountRuntimeTokens(context.Background(), []int64{1, 2}, start, end)
	if err != nil || len(values) != 2 || values[1].Tokens != "9007199254741012" || values[1].UncachedTokens != "9007199254741002" || values[2].Tokens != "0" {
		t.Fatal("window, ownership or aggregate precision wrong", values, err)
	}
}
