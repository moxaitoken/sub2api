package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type runtimeAdmin struct {
	service.AdminService
	ids []int64
	err error
}

func (s *runtimeAdmin) GetAccountsByIDs(_ context.Context, ids []int64) ([]*service.Account, error) {
	s.ids = ids
	if s.err != nil {
		return nil, s.err
	}
	out := []*service.Account{}
	for _, id := range ids {
		if id == 99 {
			continue
		}
		out = append(out, &service.Account{ID: id, Platform: "anthropic", Type: "oauth", Extra: map[string]any{"base_rpm": 15}, Credentials: map[string]any{"access_token": "never-leak"}})
		if id == 3 {
			out[len(out)-1].Extra = map[string]any{}
		}
	}
	return out, nil
}

type runtimeRPM struct {
	service.RPMCache
	fail bool
}

func (s *runtimeRPM) GetRuntimeRPM(context.Context, []int64) (map[int64]int, error) {
	if s.fail {
		return nil, errors.New("private-source-error")
	}
	return map[int64]int{1: 0, 2: 7}, nil
}

type runtimeConcurrency struct {
	service.ConcurrencyCache
	fail bool
}

func (s *runtimeConcurrency) GetAccountConcurrencyBatch(context.Context, []int64) (map[int64]int, error) {
	if s.fail {
		return nil, errors.New("private-source-error")
	}
	return map[int64]int{1: 0, 2: 3}, nil
}

type runtimeUsage struct{ service.UsageLogRepository }

func (*runtimeUsage) GetAccountRuntimeTokens(_ context.Context, ids []int64, start, end time.Time) (map[int64]service.AccountRuntimeTokens, error) {
	if end.Sub(start) != time.Minute {
		return nil, errors.New("wrong window")
	}
	return map[int64]service.AccountRuntimeTokens{1: {Tokens: "9007199254740993", UncachedTokens: "6"}, 2: {Tokens: "0", UncachedTokens: "0"}}, nil
}
func TestAccountRuntimeBatchValuesNullFailuresAndValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	admin := &runtimeAdmin{}
	rpm := &runtimeRPM{}
	conc := &runtimeConcurrency{}
	h := &AccountHandler{adminService: admin, rpmCache: rpm, concurrencyService: service.NewConcurrencyService(conc), accountUsageService: service.NewAccountUsageService(nil, &runtimeUsage{}, nil, nil, nil, nil, nil, nil, nil, nil, nil)}
	router := gin.New()
	router.POST("/runtime", h.GetRuntimeBatch)
	call := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/runtime", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	w := call(`{"account_ids":[1,2,1,99]}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, []int64{1, 2, 99}, admin.ids)
	var body struct {
		Data struct {
			RPMWindow string                          `json:"rpm_window"`
			Accounts  map[string]accountRuntimeSample `json:"accounts"`
		}
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "current_minute", body.Data.RPMWindow)
	require.Len(t, body.Data.Accounts, 2)
	require.Equal(t, 0, *body.Data.Accounts["1"].RPM)
	require.Equal(t, 3, *body.Data.Accounts["2"].Concurrency)
	require.Equal(t, "9007199254740993", *body.Data.Accounts["1"].Tokens)
	require.NotContains(t, w.Body.String(), "never-leak")
	require.NotContains(t, w.Body.String(), "credentials")
	w = call(`{"account_ids":[3]}`)
	require.Equal(t, 200, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Nil(t, body.Data.Accounts["3"].RPM)
	rpm.fail = true
	conc.fail = true
	h.accountUsageService = nil
	w = call(`{"account_ids":[1]}`)
	require.Equal(t, 200, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Nil(t, body.Data.Accounts["1"].RPM)
	require.Nil(t, body.Data.Accounts["1"].Concurrency)
	require.Nil(t, body.Data.Accounts["1"].Tokens)
	require.NotContains(t, w.Body.String(), "private-source-error")
	for _, input := range []string{`{}`, `{"account_ids":[0]}`, `{"account_ids":[-1]}`, `{"account_ids":[` + strings.Repeat("1,", 100) + `1]}`} {
		require.Equal(t, 400, call(input).Code)
	}
	admin.err = errors.New("secret-db")
	w = call(`{"account_ids":[1]}`)
	require.Equal(t, 503, w.Code)
	require.NotContains(t, w.Body.String(), "secret-db")
}
