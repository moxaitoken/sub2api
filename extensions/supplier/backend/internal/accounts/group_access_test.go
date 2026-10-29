package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/moxaitoken/sub2api-supplier/internal/adapters/sub2api"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

type groupReadStore struct {
	ports.AccountStore
	ports.OnboardingStore
}

type groupIDPolicy struct {
	ports.GroupAccessStore
	ids []int64
}

func (p groupIDPolicy) EnabledGroupIDs(context.Context) ([]int64, error) { return p.ids, nil }

type groupNamePolicy struct {
	groupIDPolicy
	ports.GroupAccessLister
	access []ports.GroupAccess
}

func (p groupNamePolicy) EnabledGroupAccess(context.Context) ([]ports.GroupAccess, error) {
	return p.access, nil
}

func TestGroupsMissingUpstream(t *testing.T) {
	for _, mode := range []string{"names", "ids", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			statuses := map[int64]int{1: 200, 5: 200, 6: 404}
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var id int64
				if r.Method != "GET" || r.Header.Get("x-api-key") != "fixture-key" {
					t.Error("unexpected method or machine credential")
				}
				if _, err := fmt.Sscanf(r.URL.Path, "/api/v1/admin/groups/%d", &id); err != nil {
					t.Error(err)
				}
				status, ok := statuses[id]
				if !ok {
					t.Error("unexpected group", id)
					status = 500
				}
				w.WriteHeader(status)
				if status == 200 {
					_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": ports.AccountGroup{ID: id, Name: "coding", Platform: "anthropic", Status: "active"}})
				}
			}))
			defer remote.Close()
			up, err := sub2api.NewAccountClient(remote.URL, "fixture-key")
			if err != nil {
				t.Fatal(err)
			}
			s, _, _ := fixture(t)
			s.Store, s.Upstream = &groupReadStore{}, up
			s.ApprovedGroups = []int64{1, 5, 6}
			if mode == "ids" {
				s.GroupPolicies = groupIDPolicy{ids: s.ApprovedGroups}
			} else if mode == "names" {
				s.GroupPolicies = groupNamePolicy{access: []ports.GroupAccess{
					{GroupID: 1, Enabled: true, SupplierGroupName: "coding"},
					{GroupID: 5, Enabled: true, SupplierGroupName: "coding"},
					{GroupID: 6, Enabled: true, SupplierGroupName: "distill"},
				}}
			}
			ctx := context.Background()
			assertGroups := func(want []int64) {
				t.Helper()
				groups, err := s.Groups(ctx)
				if err != nil || groups == nil {
					t.Fatalf("options must load with an array, got %v: %v", groups, err)
				}
				ids := []int64{}
				for _, g := range groups {
					if g.Name != "coding" {
						t.Fatalf("stale supplier group name returned: %+v", g)
					}
					ids = append(ids, g.GroupIDs...)
				}
				if !reflect.DeepEqual(ids, want) || (mode == "names" && len(want) > 0 && len(groups) != 1) {
					t.Fatalf("groups=%+v, want IDs %v", groups, want)
				}
			}
			assertGroups([]int64{1, 5})
			statuses[1] = 404
			assertGroups([]int64{5})
			// An already selected multi-group authorization must not silently shrink.
			if err := s.checkGroups(ctx, []int64{1, 5}); !errors.Is(err, ports.ErrForbidden) {
				t.Fatalf("deleted selection accepted: %v", err)
			}
			if _, err := s.BeginLocal(ctx, ports.SupplierRecord{}, ports.BeginOnboarding{GroupIDs: []int64{1, 5}}, "fixture-key"); !errors.Is(err, ports.ErrForbidden) {
				t.Fatalf("stale selection reached reservation or OAuth: %v", err)
			}
			if err := s.checkGroups(ctx, []int64{5}); err != nil {
				t.Fatalf("remaining group rejected: %v", err)
			}
			statuses[5] = 404
			assertGroups([]int64{})
			for _, status := range []int{401, 403, 429, 500, 503} {
				statuses[1] = status
				_, err := s.Groups(ctx)
				var remoteErr *sub2api.Error
				if !errors.As(err, &remoteErr) || remoteErr.Status != status {
					t.Fatalf("HTTP %d masked as deletion: %v", status, err)
				}
			}
			remote.Close()
			if _, err := s.Groups(ctx); err == nil {
				t.Fatal("transport failure masked as empty groups")
			}
		})
	}
}
