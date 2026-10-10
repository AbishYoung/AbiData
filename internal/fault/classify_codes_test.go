package fault

import (
	"fmt"
	"testing"

	"github.com/ncruces/go-sqlite3"
)

func TestClassifyBareSQLiteCodes(t *testing.T) {
	for _, c := range []struct {
		err  error
		want Code
	}{
		{sqlite3.FULL, QuotaExceeded},
		{fmt.Errorf("commit: %w", sqlite3.ExtendedErrorCode(sqlite3.FULL)), QuotaExceeded},
		{sqlite3.BUSY, WorkspaceBusy},
		{sqlite3.CONSTRAINT_UNIQUE, DuplicateValue},
		{sqlite3.CONSTRAINT_NOTNULL, RequiredField},
	} {
		if got := Classify(c.err).Code; got != c.want {
			t.Errorf("%v → %s, want %s", c.err, got, c.want)
		}
	}
}
