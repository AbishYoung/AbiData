package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"abidata/internal/dbx"
)

// Load reads the full catalog snapshot.
func Load(ctx context.Context, q dbx.Querier) (*Schema, error) {
	s := &Schema{}
	var ver string
	if err := q.QueryRow(ctx, sqlCacheGet.SQL).Scan(&ver); err != nil && !dbx.IsNoRows(err) {
		return nil, err
	}
	s.Version, _ = strconv.ParseInt(ver, 10, 64)

	rows, err := q.Query(ctx, sqlLoad.For(dialectOf(q)))
	if err != nil {
		return nil, err
	}
	idx := map[string]int{}
	for rows.Next() {
		var t Table
		if err := rows.Scan(&t.ID, &t.PhysicalName, &t.DisplayName, &t.Description, &t.TrackHistory, &t.Version); err != nil {
			rows.Close()
			return nil, err
		}
		idx[t.ID] = len(s.Tables)
		s.Tables = append(s.Tables, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = q.Query(ctx, sqlLoad2.SQL)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c Column
		var opts string
		var def, val, valText, lrel, ldisp *string
		if err := rows.Scan(&c.ID, &c.TableID, &c.PhysicalName, &c.DisplayName, &c.Type, &opts, &c.Required,
			&def, &val, &valText, &lrel, &ldisp, &c.Description, &c.Ordinal, &c.Hidden); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal([]byte(opts), &c.Options); err != nil {
			rows.Close()
			return nil, fmt.Errorf("catalog: column %s type_options: %w", c.ID, err)
		}
		c.DefaultSQL, c.ValidationSQL, c.ValidationText = deref(def), deref(val), deref(valText)
		c.LookupRelationshipID, c.LookupDisplayColumnID = deref(lrel), deref(ldisp)
		i, ok := idx[c.TableID]
		if !ok {
			continue
		}
		s.Tables[i].Columns = append(s.Tables[i].Columns, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = q.Query(ctx, sqlLoad3.SQL)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var x Index
		var cols string
		if err := rows.Scan(&x.ID, &x.TableID, &x.PhysicalName, &x.Unique, &cols); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal([]byte(cols), &x.ColumnIDs); err != nil {
			rows.Close()
			return nil, err
		}
		if i, ok := idx[x.TableID]; ok {
			s.Tables[i].Indexes = append(s.Tables[i].Indexes, x)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = q.Query(ctx, sqlLoad4.For(dialectOf(q)))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var r Relationship
		if err := rows.Scan(&r.ID, &r.Name, &r.ParentTableID, &r.ChildTableID, &r.ChildColumnID, &r.OnDelete, &r.Enforce); err != nil {
			rows.Close()
			return nil, err
		}
		s.Relationships = append(s.Relationships, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range s.Tables {
		sort.SliceStable(s.Tables[i].Columns, func(a, b int) bool { return s.Tables[i].Columns[a].Ordinal < s.Tables[i].Columns[b].Ordinal })
	}
	for i := range s.Tables {
		t := &s.Tables[i]
		for j := range t.Columns {
			t.Columns[j].ExprIssues = ExprIssues(t.Columns, t.Columns[j])
		}
	}
	s.index()
	return s, nil
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
