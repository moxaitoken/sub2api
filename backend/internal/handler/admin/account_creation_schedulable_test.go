package admin

import (
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccountCreateForwardsAtomicSchedulingChoice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, choice := range []string{"", `,"schedulable":false`, `,"schedulable":true`} {
		t.Run(choice, func(t *testing.T) {
			svc := newStubAdminService()
			h := NewAccountHandler(svc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			router := gin.New()
			router.POST("/accounts", h.Create)
			body := `{"name":"fixture","platform":"anthropic","type":"oauth","credentials":{"access_token":"synthetic"}` + choice + `}`
			r := httptest.NewRequest(http.MethodPost, "/accounts", strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Len(t, svc.createdAccounts, 1)
			if choice == "" {
				require.Nil(t, svc.createdAccounts[0].Schedulable)
			} else {
				require.NotNil(t, svc.createdAccounts[0].Schedulable)
				require.Equal(t, strings.Contains(choice, "true"), *svc.createdAccounts[0].Schedulable)
			}
		})
	}
}
