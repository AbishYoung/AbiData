// Command exprdocs writes the expression reference page and the function list
// the expression editor completes from, both from internal/exprlang (the
// single source of truth, ADR 0013 §4).
package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"

	"abidata/internal/exprlang"
)

func main() {
	md := flag.String("md", "expressions.md", "reference page path")
	js := flag.String("json", "expr-functions.json", "function list path")
	flag.Parse()
	if err := os.WriteFile(*md, []byte(exprlang.Markdown()), 0o644); err != nil {
		log.Fatal(err)
	}
	b, err := json.MarshalIndent(exprlang.Docs(), "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*js, append(b, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
}
