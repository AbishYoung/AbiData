package reports

import (
	"strings"
	"testing"

	"abidata/internal/catalog"
)

func imageLayout(c ReportControl) (*ReportLayout, *PageSetup) {
	l := &ReportLayout{Width: 600}
	l.Bands.Detail = Band{Height: 120, Controls: []ReportControl{c}}
	return l, &PageSetup{Paper: "letter", Orientation: "portrait"}
}

func TestValidateImageControl(t *testing.T) {
	tbl := &catalog.Table{Columns: []catalog.Column{
		{ID: "photos", Type: catalog.Attachments},
		{ID: "name", Type: catalog.ShortText},
	}}
	base := ReportControl{ID: "i1", Name: "Photo", Type: "image", W: 100, H: 100}

	ok := base
	ok.Bind = &Bind{ColumnID: "photos"}
	ok.MaxImages = 4
	l, ps := imageLayout(ok)
	if err := Validate(l, ps, tbl, nil); err != nil {
		t.Fatalf("a bound image control is valid: %v", err)
	}
	unbound := base
	wrongType := base
	wrongType.Bind = &Bind{ColumnID: "name"}
	tooMany := ok
	tooMany.MaxImages = MaxImagesLimit + 1
	for name, c := range map[string]ReportControl{"unbound": unbound, "wrong type": wrongType, "too many": tooMany} {
		l, ps := imageLayout(c)
		if err := Validate(l, ps, tbl, nil); err == nil || !strings.Contains(err.Error(), "image") {
			t.Errorf("%s: want an image error, got %v", name, err)
		}
	}
}
