// Package records is the row API: typed reads and writes over user tables with
// optimistic concurrency, idempotency, row history and the change feed.
package records

// Filter is the datasheet/query filter model (brief <records_and_occ>). A node is
// either a group (exactly one of And/Or non-empty) or a condition (Col + Op).
// It is compiled server-side to SQL with bound parameters; it never carries SQL.
type Filter struct {
	And   []Filter `json:"and,omitempty"`
	Or    []Filter `json:"or,omitempty"`
	Col   string   `json:"col,omitempty"` // column ID (ULID) or "id"
	Op    FilterOp `json:"op,omitempty"`
	Value any      `json:"value,omitempty"` // JSON value in the column's API representation; []any for in/between
}

// FilterOp is a comparison operator.
type FilterOp string

const (
	OpEq       FilterOp = "eq"
	OpNe       FilterOp = "ne"
	OpLt       FilterOp = "lt"
	OpLe       FilterOp = "le"
	OpGt       FilterOp = "gt"
	OpGe       FilterOp = "ge"
	OpContains FilterOp = "contains"
	OpStarts   FilterOp = "starts"
	OpEnds     FilterOp = "ends"
	OpIn       FilterOp = "in"
	OpNotIn    FilterOp = "not_in"
	OpBetween  FilterOp = "between"
	OpIsNull   FilterOp = "is_null"
	OpNotNull  FilterOp = "not_null"
	OpWithin   FilterOp = "within" // geo_point inside a GeoJSON area (ADR 0010)
	OpNear     FilterOp = "near"   // geo_point within metres of a point (ADR 0010)
)

// Sort orders rows by a column (ID or "id").
type Sort struct {
	Col  string `json:"col"`
	Desc bool   `json:"desc,omitempty"`
}

// Filter limits.
const (
	MaxFilterDepth      = 8
	MaxFilterConditions = 100
	MaxInValues         = 500
	MaxSorts            = 8
)
