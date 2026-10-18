package pgdb

import (
	"context"
	"strings"
	"testing"

	"abidata/internal/pgtest"
)

func TestCheckServer(t *testing.T) {
	e := pgtest.New(t)
	ctx := context.Background()
	d := Pool(e.App)
	if err := CheckServer(ctx, d); err != nil {
		t.Fatalf("the test app role (owner, CREATEROLE) is refused: %v", err)
	}
	e.Superuser(t, `ALTER ROLE `+Ident(e.Role)+` NOCREATEROLE`)
	if err := CheckServer(ctx, d); err == nil || !strings.Contains(err.Error(), "CREATEROLE") {
		t.Fatalf("without CREATEROLE: %v", err)
	}
	e.Superuser(t, `ALTER ROLE `+Ident(e.Role)+` CREATEROLE`, `ALTER DATABASE `+Ident(e.Database)+` OWNER TO CURRENT_USER`)
	if err := CheckServer(ctx, d); err == nil || !strings.Contains(err.Error(), "does not own") {
		t.Fatalf("not the owner: %v", err)
	}
}
