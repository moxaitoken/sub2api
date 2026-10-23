package sub2api

import (
	"context"
	"net/http"
	"testing"

	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

func TestAdminReadyRequiresExplicitComplianceStatus(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"accepted", `{"required":false}`, 0},
		{"required", `{"required":true}`, 423},
		{"missing", `{}`, 502},
		{"null", `{"required":null}`, 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/admin/compliance" || r.Header.Get("Authorization") != "Bearer synthetic-main-admin" || r.Header.Get("x-api-key") != "" {
					t.Error("incorrect caller authorization")
				}
				w.Write([]byte(`{"code":0,"data":` + tc.body + `}`))
			})
			err := c.AdminReady(context.Background(), "synthetic-main-admin", ports.ClientInfo{})
			if tc.want == 0 {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			remote, ok := err.(*Error)
			if !ok || remote.Status != tc.want {
				t.Fatal("unexpected readiness error", err)
			}
		})
	}
}
