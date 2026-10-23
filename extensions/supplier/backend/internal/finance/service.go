package finance

import (
	"context"
	"encoding/json"
	"github.com/moxaitoken/sub2api-supplier/internal/auth"
	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Service struct {
	Store ports.FinanceStore
	Vault *auth.Vault
}
type invalid struct{}

func (*invalid) Error() string      { return "INVALID_SETTLEMENT" }
func (*invalid) StatusCode() int    { return 400 }
func (*invalid) ReasonCode() string { return "INVALID_SETTLEMENT" }

var amountPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,16})(\.[0-9]{1,2})?$`)

// ParseAmount never rounds, accepts exponents, or passes money through a float.
func ParseAmount(value string) (int64, error) {
	if !amountPattern.MatchString(value) {
		return 0, &invalid{}
	}
	pieces := strings.Split(value, ".")
	fraction := "00"
	if len(pieces) == 2 {
		fraction = (pieces[1] + "0")[:2]
	}
	n, e := strconv.ParseInt(pieces[0]+fraction, 10, 64)
	if e != nil || n <= 0 {
		return 0, &invalid{}
	}
	return n, nil
}
func (s *Service) Settle(ctx context.Context, actor int64, supplier, key string, in ports.SettlementInput) (ports.Settlement, bool, error) {
	var zero ports.Settlement
	if actor <= 0 {
		return zero, false, ports.ErrForbidden
	}
	amount, e := ParseAmount(in.Amount)
	if e != nil {
		return zero, false, e
	}
	switch in.Method {
	case "usd_transfer", "cny_transfer", "usdt_transfer", "other":
	default:
		return zero, false, &invalid{}
	}
	if !utf8.ValidString(in.Reference) || !utf8.ValidString(in.Remark) || utf8.RuneCountInString(in.Reference) > 200 || utf8.RuneCountInString(in.Remark) > 1000 {
		return zero, false, &invalid{}
	}
	payload, _ := json.Marshal([]any{supplier, amount, in.Method, in.Reference, in.Remark})
	return s.Store.Settle(ctx, ports.SettlementCommand{SupplierID: supplier, ActorID: actor, Key: key, Hash: s.Vault.Fingerprint("supplier-settlement", payload), Cents: amount, Input: in})
}
