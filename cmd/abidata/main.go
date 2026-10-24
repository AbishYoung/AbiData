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
                            create a platform admin (password read from stdin or ABIDATA_PASSWORD)
  user reset-password --email E
                            set a new password for a user and sign them out everywhere
                            (password read from stdin or ABIDATA_PASSWORD; server stopped)
  demo --owner EMAIL [--kind traders|invasives|all]
                            create the sample workspaces (default: both) for an
                            existing user (the server must be stopped)
  migrate-backend --to postgres|sqlite --target-data-dir DIR [--target-backup-dir DIR]
                            move a stopped install to the other storage backend, into a
                            new data directory (and, for postgres, the database named by
                            ABIDATA_TARGET_POSTGRES_URL); the source is left as it was
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
	case "demo":
		err = demoCmd(os.Args[2:])
	case "migrate-backend":
		err = migrateBackendCmd(os.Args[2:])
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
