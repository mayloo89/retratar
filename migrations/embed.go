// Package migrations holds the schema history as embedded SQL.
//
// The files are embedded rather than read from disk so that the deployed
// artefact is a single binary: a migration cannot be missing at runtime because
// somebody copied the executable without the directory next to it.
package migrations

import "embed"

// FS holds every migration, in the format goose expects.
//
//go:embed *.sql
var FS embed.FS
