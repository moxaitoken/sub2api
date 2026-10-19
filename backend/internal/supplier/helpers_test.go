package supplier

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestModelFamily(t *testing.T) {
	cases := map[string]string{
		"claude-opus-5-5":             FamilyOpus,
		"claude-opus-4-1-20250805":    FamilyOpus,
		"claude-fable-5-1":            FamilyFable,
		"us.anthropic.claude-fable-5": FamilyFable,
		"claude-sonnet-5-5":           FamilySonnet,
		"Claude-Haiku-5-5":            FamilyHaiku,
		"gpt-6-sol":                   FamilyOther,
		"":                            FamilyOther,
	}
	for model, want := range cases {
		require.Equal(t, want, ModelFamily(model), model)
	}
}

func TestGroupByFamilyKeepsStableOrder(t *testing.T) {
	rows := []UsageRow{
		{Model: "gpt-6-sol", UsageTotals: UsageTotals{RequestCount: 1}},
		{Model: "claude-fable-5-1", UsageTotals: UsageTotals{RequestCount: 2}},
		{Model: "claude-opus-5-5", UsageTotals: UsageTotals{RequestCount: 3}},
	}
	got := groupByFamily(rows)
	require.Len(t, got, 3)
	require.Equal(t, []string{FamilyOpus, FamilyFable, FamilyOther}, []string{got[0].Family, got[1].Family, got[2].Family})
}

func TestSessionOwners(t *testing.T) {
	owners := newSessionOwners()
	now := time.Now()
	owners.put("s1", 1, AuthModeOAuth, now)

	mode, ok := owners.lookup("s1", 1, now)
	require.True(t, ok)
	require.Equal(t, AuthModeOAuth, mode)

	_, ok = owners.lookup("s1", 2, now)
	require.False(t, ok, "other suppliers cannot use the session")

	_, ok = owners.lookup("s1", 1, now.Add(31*time.Minute))
	require.False(t, ok, "sessions expire with the upstream TTL")

	owners.drop("s1")
	_, ok = owners.lookup("s1", 1, now)
	require.False(t, ok)
}

func TestValidatePublicProxyHost(t *testing.T) {
	ctx := context.Background()
	for _, host := range []string{"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254",
		"100.64.0.1", "0.0.0.0", "::1", "[::1]", "fc00::1", "fe80::1", "", "localhost"} {
		require.Error(t, validatePublicProxyHost(ctx, host), host)
	}
	for _, host := range []string{"203.0.113.10", "8.8.8.8", "2001:4860:4860::8888"} {
		require.NoError(t, validatePublicProxyHost(ctx, host), host)
	}
	require.True(t, isPublicIP(net.ParseIP("100.128.0.1")), "outside 100.64.0.0/10")
}

func TestParseBound(t *testing.T) {
	start, err := parseBound("2026-10-01", false)
	require.NoError(t, err)
	end, err := parseBound("2026-10-31", true)
	require.NoError(t, err)
	require.Equal(t, 31*24*time.Hour, end.Sub(start), "date end bounds are inclusive")

	ts, err := parseBound("2026-10-01T08:00:00Z", true)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC), ts.UTC(), "timestamps are used as-is")

	_, err = parseBound("10/01/2026", false)
	require.Error(t, err)
}

func TestRequireSupplierMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	f := newFixture(t)
	h := NewHandler(f.svc)

	serve := func(userID int64) *httptest.ResponseRecorder {
		r := gin.New()
		r.GET("/me", func(c *gin.Context) {
			if userID > 0 {
				c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: userID})
			}
			c.Next()
		}, h.RequireSupplier, h.Me)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/me", nil))
		return w
	}

	require.Equal(t, http.StatusOK, serve(10).Code)
	require.Equal(t, http.StatusForbidden, serve(999).Code, "non-suppliers are rejected")
	require.Equal(t, http.StatusUnauthorized, serve(0).Code)

	f.sup.Status = StatusDisabled
	require.Equal(t, http.StatusForbidden, serve(10).Code, "disabled suppliers are rejected")
}
