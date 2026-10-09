// Command abidata is the AbiData server and its operational CLI.
package main

import (
	"fmt"
	"os"
)

var version = "dev"

const usage = `usage: abidata <command> [flags]

commands:
  serve                     run the server
  healthcheck               exit 0 if the local server answers /healthz
  user create-admin --email E [--name N]
                            create a platform admin (password read from stdin)
  version                   print the version
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve()
	case "healthcheck":
		err = healthcheck()
	case "user":
		err = userCmd(os.Args[2:])
	case "version":
		fmt.Println(version)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "abidata:", err)
		os.Exit(1)
	}
}
