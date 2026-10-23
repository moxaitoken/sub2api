package ports

import (
	"context"
	"time"
)

type SettlementInput struct {
	Amount    string `json:"amount_usd"`
	Method    string `json:"payment_method"`
	Reference string `json:"reference"`
	Remark    string `json:"remark"`
}
type Settlement struct {
	ID           string    `json:"id"`
	LedgerID     string    `json:"ledger_id"`
	SupplierID   string    `json:"supplier_id"`
	SupplierName string    `json:"supplier_name"`
	Login        string    `json:"login_account"`
	Amount       string    `json:"amount_usd"`
	Before       string    `json:"before_usd"`
	After        string    `json:"after_usd"`
	Settled      string    `json:"settled_usd"`
	Method       string    `json:"payment_method"`
	Reference    string    `json:"reference"`
	Remark       string    `json:"remark"`
	ActorID      int64     `json:"actor_id"`
	CreatedAt    time.Time `json:"created_at"`
}
type SettlementCommand struct {
	SupplierID, Key, Hash string
	ActorID, Cents        int64
	Input                 SettlementInput
}
type FinanceQuery struct {
	SupplierID, Search string
	Page, Size         int
}
type FinanceOverview struct {
	Unsettled string `json:"unsettled_usd"`
	Settled   string `json:"settled_usd"`
	Count     int64  `json:"settlement_count"`
	Accrual   string `json:"accrual_status"`
}
type FinanceReport struct {
	Overview FinanceOverview `json:"overview"`
	Items    []Settlement    `json:"items"`
	Total    int64           `json:"total"`
	Page     int             `json:"page"`
	Size     int             `json:"page_size"`
	Pages    int64           `json:"pages"`
}
type FinanceStore interface {
	Settle(context.Context, SettlementCommand) (Settlement, bool, error)
	Finance(context.Context, FinanceQuery) (FinanceReport, error)
	Settlement(context.Context, string) (Settlement, error)
}
