package httpapi

import (
	"context"
	"testing"

	"abidata/internal/fault"
)

// A fault can override the registry title and recovery, so a bad file is not reported as "Invalid request".
func TestToAPIErrorUsesTitleAndRecoveryOverrides(t *testing.T) {
	ae := toAPIError(context.Background(), nil, fault.FileError("The CSV file is not valid at line 3.", "Fix the file.", nil))
	if ae.Err.Title != "The file could not be read" || ae.Err.Recovery != "Fix the file." || ae.Err.Code != fault.BadRequest {
		t.Errorf("%+v", ae.Err)
	}
	plain := toAPIError(context.Background(), nil, fault.New(fault.BadRequest, "x"))
	if plain.Err.Title != "Invalid request" {
		t.Errorf("default title changed: %q", plain.Err.Title)
	}
}
