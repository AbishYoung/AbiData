// Command luaapi writes the Lua host API manifest (the single source of truth
// in the Go registry) to JSON for editor completions and docs generation.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"

	"abidata/internal/script"
	_ "abidata/internal/script/modules/all"
)

func main() {
	out := flag.String("o", "lua-api.json", "output path")
	flag.Parse()
	b, err := json.MarshalIndent(script.Manifest(), "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, append(b, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
}
