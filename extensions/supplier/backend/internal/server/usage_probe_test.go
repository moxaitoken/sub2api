package server

import (
	"github.com/moxaitoken/sub2api-supplier/internal/auth"
	"github.com/moxaitoken/sub2api-supplier/internal/repository"
	"github.com/moxaitoken/sub2api-supplier/internal/suppliers"
	"net/http/httptest"
	"testing"
)

func TestUsageRouteProbe(t *testing.T) {
	store := repository.New(nil, "", "")
	h, e := New(&auth.Service{Local: store}, Config{Origin: "https://supplier.example.invalid", Suppliers: &suppliers.Service{}, Admin: &auth.Service{Administrator: true}})
	if e != nil {
		t.Fatal(e)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "https://supplier.example.invalid/api/supplier/v1/usage", nil))
	t.Logf("usage_route_status=%d", w.Code)
	if w.Code != 401 {
		t.Fatalf("usage route must require authentication, got %d", w.Code)
	}
}
