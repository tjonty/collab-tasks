// Package db embeds the SQL migrations so the API binary can apply them on
// start. The queries next to them are inputs to sqlc, not runtime files.
package db

import "embed"

// Migrations holds the goose migrations under migrations/.
//
//go:embed migrations/*.sql
var Migrations embed.FS
