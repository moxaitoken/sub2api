//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
)

func TestOverviewMembershipAllPagesAndDeletion(t *testing.T) {
	s, owner, initial, _ := accrualFixture(t, false)
	other, _ := fixtureSupplier(t, s)
	accrualAccount(t, s, other.ID, 999)
	for i := 1; i <= 25; i++ {
		accrualAccount(t, s, owner.ID, int64(101+i))
	}
	rows, e := s.OverviewAccounts(context.Background(), owner.ID)
	if e != nil || len(rows) != 26 {
		t.Fatal("overview limited to list page", len(rows), e)
	}
	for _, v := range rows {
		if v.UpstreamID == 999 {
			t.Fatal("foreign mapping included")
		}
	}
	if _, e = s.db.Exec(`UPDATE supplier.supplier_accounts SET deleted_at=now() WHERE id=$1`, initial.ID); e != nil {
		t.Fatal(e)
	}
	rows, e = s.OverviewAccounts(context.Background(), owner.ID)
	if e != nil || len(rows) != 25 {
		t.Fatal("deleted mapping included", len(rows), e)
	}
	for _, v := range rows {
		if v.ID == initial.ID {
			t.Fatal(fmt.Sprint(v))
		}
	}
}
