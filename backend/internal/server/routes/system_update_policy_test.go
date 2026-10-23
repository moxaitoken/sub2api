//go:build unit

package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type forkUpdateServiceStub struct {
	mutations int
}

func (*forkUpdateServiceStub) CheckUpdate(context.Context, bool) (*service.UpdateInfo, error) {
	return &service.UpdateInfo{CurrentVersion: "0.2.17", LatestVersion: "0.2.18", HasUpdate: true}, nil
}
func (s *forkUpdateServiceStub) PerformUpdate(context.Context) error { s.mutations++; return nil }
func (s *forkUpdateServiceStub) Rollback() error                     { s.mutations++; return nil }
func (*forkUpdateServiceStub) ListRollbackVersions(context.Context) ([]service.RollbackVersion, error) {
	return []service.RollbackVersion{}, nil
}
func (s *forkUpdateServiceStub) RollbackToVersion(context.Context, string) error {
	s.mutations++
	return nil
}

func TestForkSystemRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &forkUpdateServiceStub{}
	h := &handler.Handlers{Admin: &handler.AdminHandlers{System: adminhandler.NewSystemHandler(stub, nil)}}
	router := gin.New()
	// This local fixture models the existing outer admin gate. No real identity,
	// GitHub download, operation store, service restart or database is used.
	group := router.Group("/api/v1/admin", func(c *gin.Context) {
		switch c.GetHeader("X-Test-Role") {
		case "admin":
			c.Next()
		case "user":
			c.AbortWithStatus(http.StatusForbidden)
		default:
			c.AbortWithStatus(http.StatusUnauthorized)
		}
	})
	registerSystemRoutes(group, h)
	for _, route := range []string{"update", "rollback"} {
		for _, body := range []string{"", `{"version":"0.2.16"}`, "{"} {
			t.Run(route+"/"+body, func(t *testing.T) {
				r := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system/"+route, strings.NewReader(body))
				r.Header.Set("X-Test-Role", "admin")
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Idempotency-Key", "same-key-as-before-deployment")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, r)
				t.Logf("POST %s body=%q status=%d response=%s", route, body, w.Code, w.Body.String())
				require.Equal(t, http.StatusForbidden, w.Code)
				var envelope struct {
					Code   int    `json:"code"`
					Reason string `json:"reason"`
				}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
				require.Equal(t, http.StatusForbidden, envelope.Code)
				require.Equal(t, "IN_PLACE_UPDATE_DISABLED", envelope.Reason)
			})
		}
		for _, role := range []string{"", "user"} {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system/"+route, nil)
			r.Header.Set("X-Test-Role", role)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if role == "" {
				require.Equal(t, http.StatusUnauthorized, w.Code)
			} else {
				require.Equal(t, http.StatusForbidden, w.Code)
			}
		}
	}
	require.Zero(t, stub.mutations)
	for _, route := range []string{"version", "check-updates?force=true", "rollback-versions"} {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/"+route, nil)
		r.Header.Set("X-Test-Role", "admin")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		require.Equal(t, http.StatusOK, w.Code, route)
	}
	// Inspect the registered chain instead of invoking the real restart handler.
	for _, route := range router.Routes() {
		if route.Path == "/api/v1/admin/system/restart" {
			require.Contains(t, route.Handler, "RestartService")
			return
		}
	}
	t.Fatal("existing restart route was removed")
}
