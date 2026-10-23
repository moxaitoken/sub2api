package ports

import "context"

type SupplierRecord struct {
	Supplier                        Supplier
	Login                           string
	PasswordHash                    string
	CredentialVersion, SessionEpoch int64
}
type ManagedSupplier struct {
	Supplier
	LoginAccount string `json:"login_account"`
	Unsettled    string `json:"unsettled_usd"`
	Settled      string `json:"settled_usd"`
	Total        string `json:"total_consumption_usd"`
}
type SupplierMutation struct {
	ID, Action, Key, Hash, Name, Login, PasswordHash, Status string
	ActorID, Version                                         int64
}
type IdentityStore interface {
	Store
	LocalCredentials(context.Context, string) (SupplierRecord, error)
	LocalSupplier(context.Context, string) (SupplierRecord, error)
	ManagedSupplier(context.Context, string) (ManagedSupplier, error)
	ManagedSuppliers(context.Context, string, int, int) ([]ManagedSupplier, int64, error)
	MutateSupplier(context.Context, SupplierMutation) (ManagedSupplier, bool, error)
}
