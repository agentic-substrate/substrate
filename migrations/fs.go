// Package migrations embeds the goose SQL files applied at server start.
package migrations

import "embed"

// SQL is the goose migration filesystem. Files live next to this package so
// the binary can apply them without a checkout of the repo.
//
//go:embed *.sql
var SQL embed.FS
