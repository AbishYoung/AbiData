---
title: Queries
order: 20
summary: Build and save queries visually or in SQL, use parameters, and know who can run or save them.
---

# Queries

A query is a saved selection of data: some tables, the fields you want, filters, sorting and totals. Queries can feed datasheets, forms and reports. Builders and owners create and save queries. Everyone who can read the data can run a saved query.

## Design a query visually {#designer}

1. In the **Queries** group, choose **New query**.
2. Choose **Add table or query** to put tables and saved queries on the canvas. To join two of them, choose **Add join**, or drag from a field on one box to a field on another. In the join dialog, choose **Inner** (only rows that match on both sides) or **Left** (every row from the left table, with matching rows from the right if there are any).
3. Choose **Add field**. Pick a column, or write an expression instead. Then set the field's options in the field grid: **Output name**, **Show**, **Sort** and **Criteria**. Sort is **(not sorted)**, **Ascending** or **Descending**.
4. Choose **Run** to see the results in a dialog. Choose **Save** when it looks right, or **Save as** to save a copy under a new name and keep the original.

Criteria use these operators: *equals*, *does not equal*, *less than*, *at most*, *greater than*, *at least*, *like* (with `*` and `?` wildcards), *contains*, *starts with*, *ends with*, *is one of*, *between*, *is empty* and *is not empty*.

### Totals {#totals}

Tick **Totals** above the field grid to group records. Each field then has a **Total** setting: **Group By**, **Sum**, **Avg**, **Count**, **Min** or **Max**.

### Expressions {#expressions}

A field can be an [expression](/docs/reference/expressions) instead of a column. For example, `Quantity * Unit_price` uses the SQL names described in [Tables](/docs/guide/tables#design). When the query has several sources, write `o.Quantity` to name a source's field. Expressions in queries may also total (`sum(o.Quantity)`, `count(*)`) and use the current date. Only builders and owners can run or save queries that contain expressions.

## SQL view {#sql}

The **SQL** view shows the SQL that the designer generated, under the heading **SQL (read-only)**. You cannot edit it there.

To edit the SQL yourself, open the query's design and choose **Convert to SQL**. This button is available for a saved query with no errors. Confirm the dialog: the query becomes a SQL query and can no longer be edited visually. This cannot be undone.

A query that is written in SQL has only the **Datasheet** and **SQL** views. Its SQL editor is its design, so there is no **Design** view.

Builders can also open the **SQL console** from the **Queries** group. It runs a statement without saving it, and **Save query** keeps it as a SQL query.

SQL is written in the dialect of the database the install runs on: SQLite, or PostgreSQL on a [PostgreSQL install](/docs/operations/postgresql). The SQL editor shows which one next to its toolbar. Visual queries work the same on both. In PostgreSQL, unquoted names fold to lower case, so quote mixed-case table and field names: `SELECT "Company" FROM "Customers"`. A saved SQL query from an install on the other backend, for example after a move with `abidata migrate-backend`, shows a warning until you check it.

SQL queries are read-only. These are allowed:

- `SELECT`, `WITH` and `VALUES` statements.
- Reading your own tables.
- Built-in SQL functions.

These are refused with [SQL_NOT_ALLOWED](/docs/reference/errors#sql-not-allowed): `ATTACH`, `DETACH`, any write or schema change, `PRAGMA` (except a few read-only ones), and reading internal `_sys_` tables.

### Currency in SQL {#sql-currency}

Currency fields are stored as whole numbers of ten-thousandths, so in SQL a price of 19.99 is `199900`. The results grid shows a currency field, and a plain `SUM`, `TOTAL`, `AVG`, `MIN` or `MAX` of one (for example `SUM(Total)`), as currency for you. For anything else, such as arithmetic or an aggregate with an alias, divide by `10000.0` yourself: `SUM(Total) / 10000.0 AS revenue`.

## Locations and areas {#geo}

Queries over [Location and Area fields](/docs/guide/tables#geo) can use the `geo_*` SQL functions in expressions and in SQL queries:

```sql
SELECT s.Species, z.Zone
FROM Sightings s JOIN Zones z ON geo_within(s.Spot, z.Fence)
WHERE geo_distance_m(s.Spot, geo_point(51.5, -0.14)) < 500
```

In the designer, choose **Add join** and set **Match rows where** to **Point within area…**: the join keeps pairs where the location field of one source lies inside the area field of the other. It is offered when one column is a Location and the other an Area. For a location field, the criterion **is within** compares it with an area you draw on the map (**Draw area…**), an Area parameter, or an Area field of another source on the canvas. In the visual query model (see the OpenAPI document) these are the join condition `within` and the criterion `within`. All of these compile to `geo_within`. Result columns that hold locations or areas are typed `geo_point` and `geo_area` in the response's `column_types`, and the results grid shows them like the datasheet.

A spatial join compares every location with every area, so it is slow for very large tables on both sides.

## Parameters {#parameters}

Parameters ask the person running the query for a value, like an Access parameter query.

- In a visual query, open the **Parameters** panel and choose to add a parameter. Give it a **Name** and a **Type**: **Text**, **Whole number**, **Number**, **Currency**, **Yes/No**, **Date** or **Date and time**. Then use the parameter in a criterion. A parameter of type **Area** takes a GeoJSON Polygon or MultiPolygon (see [Locations and areas](#geo)); when the query runs, **Draw on map…** lets you draw it.
- In SQL, write a named placeholder such as `:customer`. The SQL editor finds it and lists it for you to fill in.

When you run the query, a dialog asks for each value. Values are bound to the query, never pasted into the SQL text.

## Limits {#limits}

- A query stops after 10 seconds.
- A run returns at most 50,000 rows.
- Result size is also capped. If you hit a cap, add criteria, or use a [job](/docs/guide/jobs) to process the data in a script.

## Who can run and who can save {#permissions}

| Action | Viewer | Editor | Builder | Owner |
|---|---|---|---|---|
| Run a saved query | Yes | Yes | Yes | Yes |
| Create a query, or open its design and run it unsaved | | | Yes | Yes |
| Use expressions in a visual query | | | Yes | Yes |
| Save, rename or delete a query | | | Yes | Yes |
| SQL console and SQL queries | | | Yes | Yes |

Viewers and editors do not see **New query** and cannot open a query's design. To change a query, ask a builder.

## Editing in a query's datasheet {#editing}

A query's datasheet is editable only when the query reads a single table and selects both `id` and `_version`. Any other query is read-only.

## Renaming and dependencies {#dependencies}

Before you rename or remove a table or field, **Preview changes** lists the queries that use it. A change that would break a saved query is blocked until you confirm. See [Tables](/docs/guide/tables#schema-changes).
