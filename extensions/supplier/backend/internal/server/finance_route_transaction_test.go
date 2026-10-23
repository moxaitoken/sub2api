package server

import (
	"github.com/moxaitoken/sub2api-supplier/internal/auth"
	"github.com/moxaitoken/sub2api-supplier/internal/repository"
	"github.com/moxaitoken/sub2api-supplier/internal/suppliers"
	"net/http/httptest"
	"testing"
)

func TestFinanceRouteTransaction(t *testing.T) {
	s := repository.New(nil, "", "")
	h, e := New(&auth.Service{Local: s}, Config{Origin: origin, Suppliers: &suppliers.Service{}, Admin: &auth.Service{Administrator: true}})
	if e != nil {
		t.Fatal(e)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/supplier/admin/v1/finance/ledger", nil))
	t.Logf("finance_unauthorized_status=%d", w.Code)
	if w.Code != 401 {
		t.Fatalf("want401 got%d", w.Code)
	}
}
