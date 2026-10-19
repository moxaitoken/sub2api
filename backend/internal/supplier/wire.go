package supplier

import "github.com/google/wire"

// ProviderSet wires the supplier module.
var ProviderSet = wire.NewSet(
	NewRepository,
	NewService,
	NewHandler,
)
