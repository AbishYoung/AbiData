// Package web embeds the built single-page app.
package web

import "embed"

// Dist holds the Vite build output (web/dist). dist/.gitkeep keeps the
// directory present so the binary builds before the frontend has been built.
//
//go:embed all:dist
var Dist embed.FS
