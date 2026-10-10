// Package all registers every Lua host module (blank-import it once in main).
package all

import (
	_ "abidata/internal/script/modules/db"      // db
	_ "abidata/internal/script/modules/records" // records
	_ "abidata/internal/script/modules/ui"      // event, record, ui, notify, workspace
)
