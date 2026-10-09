// Package docs embeds the documentation content rendered by the docs server.
package docs

import "embed"

// Content holds docs/content/**.
//
//go:embed all:content
var Content embed.FS
