// Package migrations embeds the plain SQL schema migrations applied on startup.
package migrations

import "embed"

// FS holds every *.sql file in this directory. Files are applied in lexical
// order, so name them with a zero-padded numeric prefix (0002_foo.sql).
//
//go:embed *.sql
var FS embed.FS
