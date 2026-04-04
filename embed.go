package flip7

import "embed"

// WebFS holds the embedded web/ static files baked into the binary at compile time.
//
//go:embed web
var WebFS embed.FS
