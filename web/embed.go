// Package web embeds static assets (compiled CSS, vendored JS, icons).
package web

import "embed"

//go:embed all:static
var Static embed.FS
