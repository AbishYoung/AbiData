# ADR 0013: A backend-agnostic SQL layer (one model, SQL generated per dialect)

Status: Proposed (plan for future implementation) · 2026-10-17 · Prerequisite of ADR 0012

## Context

ADR 0012 plans PostgreSQL as an alternative backend. Its hardest parts all come from the same fact: AbiData writes **SQLite SQL as text** in many places, and stores user-authored **SQLite SQL fragments** inside the catalog.

| Where SQL is produced | How | Size today |
|---|---|---|
| System-table access (`_sys_*`, `_adm_*`) | Hand-written SQL strings | Most of the ~210 call sites |
| User-table CRUD, filters and sorting (`internal/records`) | String building: `selectList`, `CompileFilter`, `CompileSort` | Every datasheet, form and API read and write |
| Visual queries (`internal/query/visual.go`) | String building in `Compile`, `criterion` and `sqlLiteral` | All saved visual queries, re-run through `RefreshCompiled` |
| DDL (`internal/catalog/ddl.go`) | String building: `columnDef`, `createTableSQL`, triggers | Every schema change |
| Import staging and exports (`internal/ingest`) | Mixed | Every import and export |
| Calculated fields, validation rules, defaults | **User-written SQLite SQL**, lexically checked, then vetted by the SQLite authorizer and embedded in DDL | Stored in `_sys_columns` |
| Saved SQL queries, Lua `db.query`/`db.exec` | **User-written SQLite SQL**, run under the authorizer | Stored in `_sys_queries` and in scripts |

Without a shared layer, ADR 0012 has to:
- write and maintain two versions of every dynamic SQL path;
- **translate** stored expressions and visual queries when a workspace crosses backends;
- validate stored expressions on PostgreSQL by indirect means, because PostgreSQL has no authorizer (ADR 0012 §3).

This ADR removes most of that. **Everything AbiData itself generates is built once, as a model, and rendered per dialect.** The only SQL left in a backend's dialect is SQL a person typed as SQL.

## Decision

### 1. Three layers

```
 services (records, query, catalog, forms, reports, ingest, jobs, admin, Lua modules)
        │  build statements as values, never as strings
        ▼
 internal/sqlgen   statement + expression model, logical types, function catalog,
                   dialect renderers (sqlite, postgres), static-statement registry
        │  rendered SQL text + bound args (+ result column types)
        ▼
 internal/dbx      connections, transactions, placeholders, codecs (ADR 0012 §6)
        ├── sqlitex     (today's driver, unchanged behaviour)
        └── postgres    (ADR 0012)
```

- **`sqlgen`** knows nothing about connections.
- **`dbx`** knows nothing about AbiData's schema.
- **Services** know neither driver, only the dialect the workspace runs on. They need it in one case: to say which dialect a user-typed SQL string is in.

### 2. `internal/sqlgen`: the model

**Statements are a small, closed set of Go structs.** This is an AST, not a fluent builder over strings:

- `Select`, which covers:
  - sources: a table, a subquery, or a CTE;
  - joins: inner, left, and the spatial join of ADR 0010;
  - `Where`, `GroupBy`, `Having`, `OrderBy`, `Limit`/`Offset`;
  - `Distinct`, and `Union`/`Union All`.
- `Insert` (values or select, `OnConflict` do-nothing/do-update, `Returning`), `Update` (`Returning`) and `Delete` (`Returning`).
- DDL:
  - `CreateTable`, with logical column types (§3), named constraints, generated columns, identity key and `_version`;
  - `AlterTable`: add, drop and rename column; rename table;
  - the table-rebuild recipe for changes SQLite can't alter in place;
  - `CreateIndex` (unique, partial, case-insensitive) and `Drop*`.
- **Behavioural triggers** as named intents rather than trigger bodies: `OCCBump(table)` and `AttachmentRefs(table, column)`. Each renderer emits that backend's trigger: SQLite `AFTER UPDATE` / PostgreSQL PL/pgSQL `BEFORE UPDATE` (ADR 0012 §4).

**Expressions** are a small closed set:
- `Col(alias, column)`, `Param(name, logicalType)`, `Lit(value, logicalType)`;
- comparisons, `And`/`Or`/`Not`, arithmetic and `Concat`;
- `IsNull`, `In`, `Between`, `Case`;
- `Call(fn, args…)`, where `fn` comes from the function catalog (§4);
- `Match(kind, expr, pattern, caseInsensitive)`, where kind is like / contains / starts / ends, with escaping done by the renderer;
- `Cast(expr, logicalType)`;
- aggregates (`Count`, `Sum`, `Avg`, `Min`, `Max`) and `Exists`/subquery.

**Rendering.** `Render(dialect, node) → (sql string, args []any, columns []ResultColumn)`:
- Rendering is deterministic.
- Identifiers are always quoted by the renderer, so injection through names is impossible by construction.
- Values are always bound parameters, except the literals that DDL requires (defaults, CHECK constants). Those go through one audited literal encoder per dialect.
- `ResultColumn` carries the **logical type** of every output column. This replaces today's mix of `ColumnOrigin`, declared types and value sampling (`query.originType`, `sampleTypes`, `aggregateOfCurrency`) for generated SQL.

**Why not an existing builder** (`squirrel`, `goqu`, `bob`, `sqlc`)? Each covers DML text generation for a subset of dialects. None covers the parts that make this hard:
- logical types with codecs;
- a function catalog with per-dialect implementations and determinism flags;
- named constraints that map errors back to fields;
- SQLite's table-rebuild DDL;
- trigger intents;
- typed result columns.

Also, `sqlc` generates from SQL text, which is exactly what we are moving away from. The model is small: the visual compiler, filters, CRUD and DDL use perhaps 40 node types. Owning it costs less than bending a library.

### 3. Logical types and codecs

One type system above the backends. The field types of the catalog map onto it, and so do the result types that are already exposed as `column_types`: `text`, `integer`, `number`, `currency`, `boolean`, `date`, `datetime`, `json`, `geo_point`, `geo_area` and `attachments`.

- **Storage type per dialect.** Each logical type has one. The table is ADR 0012 §4; SQLite keeps today's types exactly.
- **A codec per (logical type, dialect)**, in `dbx`. It converts between the Go value services use and the driver value:
  - booleans are 0/1 on SQLite and `bool` on PostgreSQL;
  - dates are ISO text on SQLite and `time.Time` on PostgreSQL;
  - JSON is text on SQLite and `jsonb` on PostgreSQL;
  - currency is `int64` ×10⁴ on both (D13).

  `records` encode and decode through these codecs instead of today's SQLite-shaped `codec.go`, so the API, Lua values and exports are identical on both backends.
- **Case-insensitive text** is a property of the logical column (`text` with `ci: true`), not a collation spelled in SQL. On SQLite it renders as `COLLATE NOCASE`; on PostgreSQL as the `<prefix>_ci` collation, or `ILIKE` for matching.

### 4. The function catalog

Every function that generated SQL or a stored expression may call is a **catalog entry**. An entry has:
- a logical name and signature, such as `date_add(date, integer, unit) → date` or `geo_within(geo_point, geo_area) → boolean`;
- a result type;
- `deterministic` / `aggregate` flags;
- a **rendering per dialect**: a builtin, a rewrite into other nodes, or a call into `<prefix>_lib` on PostgreSQL / a registered Go function on SQLite;
- docs and examples, generated into the expression reference page, just as D19 already does for the Lua API and error catalog.

**It starts from today's allowlist.** `internal/catalog/expr.go`'s `deterministicFuncs` list (string, math, date, JSON and `geo_*` functions) becomes the first set of entries. The SQLite rendering is the identity. The PostgreSQL rendering is a builtin where semantics match, or a `<prefix>_lib` function where they don't.

**The catalog is an allowlist by construction.** A function that is not in it cannot be expressed, so stored expressions no longer depend on the SQLite authorizer for function vetting. On PostgreSQL they don't need ADR 0012 §3's plan-based checks either. That removes ADR 0012's riskiest spike item for everything except user-typed SQL.

### 5. A portable expression language for stored expressions

Calculated fields, validation rules and defaults stop being SQLite SQL. They become **AbiData expressions**:
- **Syntax:** a SQL-flavoured expression grammar (decided; see Decisions taken), kept close to what builders write today so most existing expressions parse unchanged. There are no Access-style aliases. Columns are referenced by name. It has literals, operators, `CASE`, and calls to catalog functions. It has no subqueries, no statements and no parameters.
- **Parsing and storage:** expressions are parsed in Go into the §2 expression model and type-checked against the table's columns and the function catalog. The source text and a parsed-form version are stored in `_sys_columns`.
- **Errors** point at the token and are friendly, for example "`ifnul` is not a function; did you mean `ifnull`?". The UI's expression editor gains completion from the catalog and the table's columns.
- **Rendering:** each expression is rendered per dialect into the DDL: generated columns, CHECK constraints and defaults. Determinism comes from the catalog flags, and each backend still enforces it itself.
- **What it replaces:** the lexical check (`lexCheckOpt`) and the authorizer-based semantic check (`CheckExpr`). A parser that only accepts the grammar cannot be made to close a clause or append a statement, so the lexical guard becomes redundant. It is kept one release as a belt-and-braces check.
- **Migration of existing expressions.** A one-time catalog upgrade parses every stored expression.
  - Expressions that parse are stored in the new form, with behaviour unchanged on SQLite.
  - Expressions that don't parse (SQLite-only syntax outside the grammar) keep working on SQLite, because the old text is still in the DDL. They are marked **needs attention** in the table designer, with the parse error, until a builder rewrites them.
  - Crossing to PostgreSQL requires the rewrite.

**Lua (D12).** Calculated *controls* in forms and reports stay in Lua. Unifying them with this language is out of scope; it is noted as a possibility.

### 6. What becomes dialect-neutral, and what stays dialect-specific

| Artefact | After this ADR |
|---|---|
| Visual queries | **Neutral.** `query.Compile` builds a `sqlgen.Select`; the stored model is already neutral and SQL is rendered at run time. `RefreshCompiled` / `CompilerVersion` become per dialect. |
| Records CRUD, filters, sort, lookups and row history | **Neutral.** Built as models from the catalog. |
| DDL, schema preview/apply, OCC and attachment triggers | **Neutral.** Each renderer also produces the preview's "what will happen" facts, such as "rebuilds the table" on SQLite or "rewrites the table" for a stored generated column on PostgreSQL 17. |
| Stored expressions | **Neutral** (§5). |
| Imports and exports | **Neutral.** Staging stays SQLite (ADR 0012 §8); the commit into the workspace is generated. |
| System tables | **Neutral by registry** (§7). |
| Saved SQL queries, SQL console | **Dialect-specific, by decision.** A person typed them; each records its dialect (ADR 0012 §4). There is no portable-SQL mode, so crossing backends uses ADR 0012 §8's best-effort translation and "needs attention" flow. |
| Expression columns in visual queries | **Neutral.** They become AbiData expressions (§5) in Phase D, replacing raw SQL. Raw SQL stays available through the SQL view and SQL queries. |
| Lua `db.query` / `db.exec` with SQL strings | **Dialect-specific.** A new **neutral** alternative is offered (§8). |

As a result, ADR 0012 §8's translation step shrinks to **saved SQL queries and Lua SQL strings**. Everything else moves between SQLite and PostgreSQL with no translation at all.

### 7. System tables: a statement registry

Most system-table SQL is short, static and the same on both engines once ADR 0012's convergence pass is done. Rewriting all of it as models would be churn without benefit. Instead:

- **Registration.** Static statements are declared once with `sqlgen.Static("admin.users.byEmail", "SELECT … WHERE email = ?")` and called through the handle, never as an inline string.
- **Conformance test.** A test **prepares every registered statement on every dialect**: SQLite in-process, and PostgreSQL in CI's container. It fails on any statement that doesn't parse or doesn't match its declared parameter count and result columns. The registry is therefore a checked inventory of portable SQL.
- **Exceptions.** A statement that genuinely differs is registered with one variant per dialect and a comment saying why. Those variants are the full list of hand-maintained dialect differences, and they are visible in one place.
- **Enforcement.** A lint rule forbids string literals passed directly to `Exec`/`Query`/`QueryRow` outside `sqlgen` and the two drivers.

**As built (Phase E):**

- **Registration.** Each package declares its statements in `statements.go`, using `sqlgen.Register(name, sql)`, which returns a `*sqlgen.Static`.
  - The name is `package.Func`, with `#n` added for a function's later statements. Call sites pass `handle.SQL`.
  - Registration panics on a duplicate name. It also panics on portable SQL that uses a construct only SQLite knows, such as PRAGMA, `INSERT OR`, `json_*`, `COLLATE NOCASE` or the date functions.
  - `sqlgen.RegisterSQLiteOnly` marks the rest: 30 of 155 at the time of writing. Their PostgreSQL variants are the counted remainder for ADR 0012 Phase 3. That replaces the per-dialect variants described above until a second dialect exists.
- **Conformance test.** `internal/statements` prepares every statement on SQLite against a migrated admin database, a workspace database and an import staging database.
  - It records which database each statement runs on, its dialect and its result columns in a golden file.
  - The PostgreSQL leg arrives with ADR 0012 Phase 1.
  - Parameter counts are not yet recorded: SQLite reports none for numbered or named placeholders in a way that would add anything.
- **Lint rule.** This is a Go test, not a linter plugin: an AST walk over `internal/` and `cmd/` that refuses a SQL string literal passed to `Exec`, `Query`, `QueryRow` or a package's `exec` helper.
  - Not covered: SQL assembled at run time, which is 42 call sites that quote identifiers into the text, and migration scripts (`ExecScript`).
  - The run-time SQL is left to the models in Phase F.

### 8. A neutral query API for Lua

Add a builder to the `db` module that produces the same `sqlgen.Select`/`Insert`/`Update`/`Delete` models:

```lua
db.select{ from = "Orders", where = { status = "Open", total = { gt = 100 } },
           order = { "-ordered_at" }, limit = 50 }
db.update{ table = "Orders", set = { status = "Shipped" }, where = { id = 42 } }
```

- **Sandboxing.** It runs under the same sandbox and budgets, so `Policy` (read vs write, denied tables) applies to the model before rendering. It also stays enforced by the backend's own sandbox.
- **Portability.** Scripts written this way are portable. Raw `db.query(sql)` stays available, documented as dialect-specific.
- **Generated docs.** The Lua API manifest (D19) gains the builder, so docs and completion come for free.

### 9. Semantics the layer must pin down

Generating SQL for two engines is only safe if both engines give the same *answers*. The renderers make these choices explicit instead of inheriting each engine's default:

- **NULL ordering.** SQLite puts NULLs first when sorting ascending; PostgreSQL puts them last. Every generated `ORDER BY` states `NULLS FIRST`/`LAST`, which SQLite supports since 3.30. The product rule is the datasheet's current behaviour.
- **Case-insensitive matching.** SQLite's `LIKE` ignores ASCII case by default; PostgreSQL's does not. `Match` carries an explicit flag, rendered as `LIKE` with `NOCASE` or as `ILIKE`. Non-ASCII case folding is defined by the collation, and the conformance tests below record any remaining difference.
- **Boolean, date and JSON comparisons** go through logical types, never through raw `= 1` or string compares.
- **GROUP BY strictness.** The generator always emits standard-conforming grouping, so PostgreSQL never rejects it. SQLite accepts the stricter form unchanged.
- **Integer division, numeric rounding, string-to-number coercion and text length in characters vs bytes** may differ between the engines. They are **not assumed**: each catalog function and operator gets conformance vectors (below). Where the engines disagree, the renderer emits the form that gives AbiData's documented result on both, or the function is implemented in `<prefix>_lib`/Go.
- **Identifier limits:** physical names are already capped at 63 bytes (`catalog.DerivePhysicalName`), which is PostgreSQL's limit. That cap becomes a documented rule of the layer.
- **Parameter limits:** batch sizes for multi-row inserts are chosen per dialect (SQLite 32,766 variables, PostgreSQL 65,535).

**Differential testing** keeps these honest:
- A property-based generator builds random well-typed expressions and queries from the function catalog and the node set.
- It runs them on SQLite and PostgreSQL over the same fixture data and compares the results.
- Every disagreement is either a bug or a documented, intentional difference.
- This runs in CI on a fixed seed, plus a longer randomised nightly run.

### 10. Phases

All phases deliver value on SQLite alone, before any PostgreSQL code exists, and each merges independently.

| Phase | Work | Exit criterion |
|---|---|---|
| A. Model and SQLite renderer | `sqlgen` nodes, logical types, function catalog seeded from `deterministicFuncs`, SQLite renderer, golden tests on rendered SQL plus execution tests. | Renderer covers what `visual.go`, `filter.go` and `selectList` need. |
| B. Generated dynamic SQL | Port `query.Compile`, `records` filters, sort, CRUD and lookups, row history, and ingest commit to the model; typed result columns replace `originType`/sampling for generated SQL. | Full Go suite and e2e unchanged; `column_types` identical on the demo workspace's queries. |
| C. Generated DDL | Port `catalog/ddl.go`, including the rebuild recipe and trigger intents. | Catalog, convert and upgrade tests unchanged; a byte-level schema comparison on the demo workspace shows no drift. |
| D. Expression language | Parser, type checker, editor completion, the catalog upgrade that parses existing expressions, "needs attention" marking. The visual designer's expression columns switch to AbiData expressions; saved visual queries' expressions are upgraded the same way. | Every expression in the demo and the test fixtures parses; the authorizer path is used only for user-typed SQL. |
| E. Statement registry | Move system-table SQL to `sqlgen.Static`, conformance test (SQLite now, PostgreSQL once ADR 0012 Phase 1 lands), lint rule. | No inline SQL outside `sqlgen` and the drivers. |
| F. PostgreSQL renderer and codecs | Second renderer, `<prefix>_lib` functions, codecs, and differential testing. This is the hand-off point into ADR 0012 Phase 3. | Differential tests green on PostgreSQL 17 and 18. |
| G. Lua builder | `db.select` / `insert` / `update` / `delete` on the model. | Lua API manifest updated; scripts using it pass on both backends. |

**Phase F, part 1 (as built).**

- **What renders.** `sqlgen.Render(sqlgen.Postgres, …)` renders expressions and DML.
  - Placeholders stay `?`/`:name` for `dbx.Rebind`.
  - DDL returns `ErrDialect` until part 2.
- **SQLite's answers, pinned explicitly:**
  - `IS [NOT] DISTINCT FROM`;
  - NULL for division or `%` by zero;
  - `%` on integer parts;
  - yes/no counted as 0/1 next to numbers;
  - `CAST` to integer truncates;
  - SUM of integers stays `bigint` and AVG is `double precision`;
  - MIN/MAX of yes/no is `bool_and`/`bool_or`;
  - scalar `max`/`min` are NULL if any argument is;
  - float literals are `float8`.
- **Matching.** LIKE, GLOB, `instr` and `replace` work on the text under `COLLATE "default"`, since text columns use the nondeterministic `<prefix>_ci` collation. `Match.CI` renders as `ILIKE`.
- **Functions.** Each catalog function maps to a builtin, a rewrite, or an `abi_*` function in `<prefix>_lib` (`internal/pgdb/lib`, installed by `pgdb.InstallLib`).
  - The library has SQLite's `substr`, `round` (of the double's exact value), the math functions that are NULL on domain errors, `mod`, `glob` and a 64-bit `random`.
  - It also ports SQLite's date.c: `date`, `time`, `datetime`, `julianday`, `unixepoch` and `strftime` with SQLite's modifiers and conversions. They work on text, numbers and native `date`/`timestamptz` columns, and 'now' is passed as `statement_timestamp()`.
  - Text compared with a native date column is read as a date, or as a UTC timestamp. A native date-and-time column used as text is written in `dbx.TimeLayout`, as SQLite stores it.
  - The JSON and geo functions, `timediff`, `printf`/`format`, `quote`, `typeof` and `unhex` are listed as pending. They fail to render rather than render wrongly, and a test keeps that list exact.
- **Differential tests** (`internal/pgdb/dialect_test.go`) run about 200 expressions, dates included, and the grouping, sorting, matching and paging cases on mirrored SQLite and PostgreSQL tables. They pass on PostgreSQL 18.6; CI runs 17 and 18.
  - The one recorded difference is `lower`/`upper` on non-ASCII letters: SQLite changes ASCII only.
  - Number-to-text formatting also differs (PostgreSQL writes a whole `double` as `2`, SQLite as `2.0`). The test data avoids that case, and part 2's codecs address it.

Phases A–E are worth doing even if PostgreSQL is never built:
- one typed path replaces the string-building in four packages;
- stored expressions get real parsing and good error messages instead of an authorizer probe;
- result column types become exact instead of sampled;
- system SQL becomes an audited inventory.

## Consequences

**Effect on ADR 0012.**
- Its §4 dialect differences live in `sqlgen`'s renderers and `dbx`'s codecs.
- Its §6 "converge first" pass becomes Phases A–E here.
- Its §8 translation shrinks to saved SQL and Lua SQL strings.
- Its open question 3 (a dialect-neutral expression language) is answered yes, by §5.

**Changes to settled decisions.**
- **D12** changes: column validation and calculated fields use AbiData expressions instead of raw SQL.
- **D19** gains another single source of truth (the function catalog) that generates docs.

**Costs.**
- The function catalog and conformance vectors must grow with every new function.
- The expression grammar is now a language AbiData maintains.
- Existing SQLite-only expressions outside the grammar need a builder's attention before they can move to PostgreSQL.

**Risk.** The main risk is semantic drift between the renderers, where the same model gives different answers. Differential testing (§9) exists to catch it, and it must run on every change to `sqlgen`.

## Decisions taken

Recorded 2026-10-17, from the questions this plan first left open:

1. **Expression syntax is SQL-flavoured.** No Access-style names (`IIf`, `Nz`, `DateAdd`, `[Field]`) and no second spelling. The function catalog's names are the SQL names already in today's allowlist.
2. **No portable mode for saved SQL queries.** They stay in the backend's dialect, and moving them between backends relies on ADR 0012 §8. The grammar AbiData owns stays an expression grammar, never a statement language.
3. **Expression columns in the visual query designer use AbiData expressions** (Phase D). Builders who need raw SQL use the SQL view and SQL queries.
