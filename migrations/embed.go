// Package migrations embeds the goose SQL migrations into the binary.
package migrations

import "embed"

// FS holds every file in this directory; goose only picks up the *.sql ones.
//
//go:embed *
var FS embed.FS
