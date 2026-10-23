package repository

import (
	"errors"
	"strings"
	"testing"

	"github.com/lib/pq"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
)

func TestErrorMappingDoesNotExposeConflictingValues(t *testing.T) {
	for _, code := range []pq.ErrorCode{"23505", "23503", "23514", "23502", "22P02"} {
		err := mapError(&pq.Error{Code: code, Detail: "private-other-tenant"})
		if !errors.Is(err, ports.ErrConflict) || strings.Contains(err.Error(), "private-other-tenant") {
			t.Fatalf("unsafe mapping for %s", code)
		}
	}
	opaque := errors.New("opaque database failure")
	if mapError(opaque) != opaque {
		t.Fatal("unknown errors must remain identifiable to internal caller")
	}
}

func TestHashValidation(t *testing.T) {
	if !validHash(strings.Repeat("ab", 32)) || validHash(strings.Repeat("ab", 31)) || validHash(strings.Repeat("zz", 32)) {
		t.Fatal("SHA256 hex validation")
	}
}
