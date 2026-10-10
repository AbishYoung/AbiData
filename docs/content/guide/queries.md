---
title: Queries
order: 20
summary: Build and save queries visually or in SQL, use parameters, and know who can run or save them.
---

# Queries

A query is a saved selection of data: some tables, the fields you want, filters, sorting and totals. Queries can feed datasheets, forms and reports.

## Design a query visually {#designer}

1. In the **Queries** group, choose **New query**.
2. Add tables to the canvas. Drag between matching fields to create a join. Choose **inner** (only matching records) or **left** (all records from the left table).
3. Add fields to the field grid. For each field you can set an output name, show or hide it, sort it (**Ascending** or **Descending**) and set criteria.
4. Choose **Run** to see the results in a dialog. Choose **Save** when it looks right. **Save as** keeps the original.

Criteria use operators such as *equals*, *less than*, *between*, *contains*, *starts with*, *is one of*, *is empty* and *like* (with `*` and `?` wildcards).

### Totals {#totals}

Turn on totals to group records. Each field then has a **Total** setting: **Group By**, **Sum**, **Avg**, **Count**, **Min** or **Max**.

### Expressions {#expressions}

A field can be an SQL expression instead of a column. Expressions can use built-in SQL functions, not your own tables' internals. Only builders and owners can run or save queries that contain expressions.

## SQL view {#sql}

The **SQL** view shows the SQL the designer generated. It is read-only for visual queries.

To edit the SQL yourself, choose **Convert to SQL** in the Design view. This cannot be undone: a converted query never goes back to the visual designer. The query must be saved first.

Builders can also open the **SQL console** (the button in the Queries group) to run a read-only statement without saving it, and save it as a SQL query.

SQL queries are read-only. These are allowed:

- `SELECT`, `WITH` and `VALUES` statements.
- Reading your own tables.
- Built-in SQL functions.

These are refused with [SQL_NOT_ALLOWED](/docs/reference/errors#sql-not-allowed): `ATTACH`, `DETACH`, any write or schema change, `PRAGMA` (except a few read-only ones), and reading internal `_sys_` tables.

## Parameters {#parameters}

Parameters ask the person running the query for a value, like an Access parameter query.

- In a visual query, add parameters in the **Parameters** panel and give each a name and a type (text, whole number, number, currency, yes/no, date or date and time). Use the parameter in a criterion.
- In SQL, write a named placeholder such as `:customer`. The designer finds it and lists it for you to type.

When you run the query, a dialog asks for each value. Values are bound to the query, never pasted into the SQL text.

## Limits {#limits}

- A query stops after 10 seconds.
- A run returns at most 50,000 rows.
- Result size is also capped. If you hit a cap, add criteria or use a [job](/docs/guide/jobs) to process the data in a script.

## Who can run and who can save {#permissions}

| Action | Viewer | Editor | Builder | Owner |
|---|---|---|---|---|
| Run a saved query | Yes | Yes | Yes | Yes |
| Run an unsaved visual query | | Yes, without expressions | Yes | Yes |
| Save, rename or delete a query | | | Yes | Yes |
| SQL console and SQL queries | | | Yes | Yes |

Editors can design and run a query to look at data but cannot save it. Ask a builder to save it.

## Editing in a query's datasheet {#editing}

A query's datasheet is editable only when the query reads a single table and selects both `id` and `_version`. Any other query is read-only.

## Renaming and dependencies {#dependencies}

Before you rename or remove a table or field, **Preview changes** lists the queries that use it. A change that would break a saved query is blocked until you confirm. See [Tables](/docs/guide/tables#schema-changes).
