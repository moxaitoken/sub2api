// Package migrations owns the supplier database's immutable forward migrations.
package migrations

import "embed"

// Files excludes all upstream Sub2API migrations.
//
//go:embed *.sql
var Files embed.FS
