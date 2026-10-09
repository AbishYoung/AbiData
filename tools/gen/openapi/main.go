// Command openapi writes the API's OpenAPI 3.1 document (used by `make gen`
// to generate web/src/generated/api.d.ts).
package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"

	"abidata/internal/httpapi"
)

func main() {
	out := flag.String("o", "openapi.json", "output path")
	flag.Parse()
	s := httpapi.New(httpapi.Deps{Version: "1"})
	b, err := json.MarshalIndent(s.API().OpenAPI(), "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, append(b, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
}
