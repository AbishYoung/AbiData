// Command sitegen renders the public website (home page plus the documentation)
// into a static directory for GitHub Pages. See docs/planning/public-website.md.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	cfg := Config{}
	flag.StringVar(&cfg.DocsDir, "docs", "docs/content", "markdown documentation directory")
	flag.StringVar(&cfg.ImagesDir, "images", "docs/images", "screenshots directory")
	flag.StringVar(&cfg.SiteDir, "site", "site", "site sources (index.html, docs.tmpl.html, assets/)")
	flag.StringVar(&cfg.OutDir, "out", "_site", "output directory (removed first)")
	flag.StringVar(&cfg.Base, "base", "/AbiData/", "URL path the site is served under")
	flag.Parse()
	if err := Generate(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "sitegen:", err)
		os.Exit(1)
	}
}
