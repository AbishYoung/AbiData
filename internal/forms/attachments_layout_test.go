package forms

import (
	"strings"
	"testing"

	"abidata/internal/catalog"
)

func attachmentsLayout(bind *Bind) *Layout {
	return &Layout{Width: 400, Sections: Sections{Detail: Section{Height: 200, Controls: []Control{
		{ID: "c1", Name: "Photos", Type: "attachments", X: 8, Y: 8, W: 200, H: 120, Bind: bind},
	}}}}
}

func TestValidateAttachmentsControl(t *testing.T) {
	tbl := &catalog.Table{Columns: []catalog.Column{
		{ID: "photos", Type: catalog.Attachments},
		{ID: "name", Type: catalog.ShortText},
	}}
	src := Source{Table: tbl}
	none := func(string) bool { return false }

	if err := Validate(attachmentsLayout(&Bind{ColumnID: "photos"}), src, nil, none); err != nil {
		t.Fatalf("a bound attachments control is valid: %v", err)
	}
	for name, bind := range map[string]*Bind{"unbound": nil, "wrong field type": {ColumnID: "name"}} {
		err := Validate(attachmentsLayout(bind), src, nil, none)
		if err == nil || !strings.Contains(err.Error(), "attachments") {
			t.Errorf("%s: want an attachments error, got %v", name, err)
		}
	}
}
