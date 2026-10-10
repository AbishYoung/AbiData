---
title: Scripts
order: 50
summary: Write Lua for forms, reports and jobs; know the contexts, time budgets and limits.
---

# Scripts

Scripts are written in Lua 5.1 and run in a sandbox on the server. Builders and owners create and edit them in **Script Studio**.

## Script kinds {#kinds}

| Kind | Used by |
|---|---|
| Module | Shared functions. Other scripts load it with `require("name")`. |
| Form module | Event handlers for one form. Edit it in the form designer's **Module** panel. |
| Report module | Functions for one report's expressions. |
| Job | The code a [job](/docs/guide/jobs) runs. |

Script Studio checks your code as you type. Syntax errors and unknown function names appear as markers and in the **Problems** list. Hover a function to see its documentation, with a link to the [Lua API reference](/docs/reference/lua-api). Save with `Mod+S`.

## Lua basics {#basics}

```lua
local total = 0
for _, row in ipairs(db.query("SELECT Qty, Price FROM OrderLines WHERE OrderId = :id", { id = record.id })) do
  total = total + row.Qty * row.Price
end
log.info("order total", { id = record.id, total = total })
return total
```

Things that differ from what you may expect:

- **Empty fields are `db.NULL`, not `nil`.** Lua tables cannot hold `nil`. Test with `db.is_null(v)`, which is true for both.
- **Currency fields are `decimal` values.** Use `decimal.new("12.50")` and the usual operators to avoid floating-point error.
- **Dates and times are strings.** A date is `"2026-01-31"`; a timestamp is ISO 8601 UTC such as `"2026-03-09T05:30:00.000Z"`. They sort and compare correctly as text. Use `datetime` for calendar arithmetic.
- Integers beyond 2^53 are returned as strings.
- SQL uses named parameters (`:id`) filled from a table. Do not build SQL by joining strings.
- `print` writes to the run log.
- Only `string`, `table` and `math` are available from the standard library. `os`, `io`, `debug`, `load`, `coroutine` and `package` are not.

## Contexts and budgets {#contexts}

A script runs in one of three contexts. The context decides which functions exist and how long the script may run.

| Context | Runs when | Time budget | `db.transaction` block | Longest string | Network | Writes |
|---|---|---|---|---|---|---|
| `ui_event` | A form event fires | 1 second | 250 ms | 1 MiB | No | Yes, if the user's role allows |
| `report` | A report runs | 30 seconds per run | not available | 1 MiB | No | No |
| `job` | A job runs | The job's timeout, up to 3,600 s (default 300 s) | 2 s | 16 MiB | `fetch` to allowed hosts | Yes |

Waiting for a free slot counts against the budget. If a script is too slow, you get [SCRIPT_TIMEOUT](/docs/reference/errors#script-timeout). If it builds a string that is too long, uses a runaway pattern, or the server runs low on memory, you get [SCRIPT_LIMIT_EXCEEDED](/docs/reference/errors#script-limit-exceeded). Any other failure is [SCRIPT_ERROR](/docs/reference/errors#script-error) with the line number.

Practical rules:

- Keep form handlers short. Move bulk work into a job and start it with `jobs.enqueue`.
- Form handlers run as the signed-in user. Jobs run as the job principal, with editor rights to data.
- Reports are read-only. `db.exec` is not available in them.
- A single `db.query` returns up to 10,000 rows. In jobs, use `db.iter` to read more in pages of 1,000.
- Pattern matching (`string.find`, `match`, `gmatch`, `gsub`) stops after 1,000,000 steps.

Choose the right function for the change:

| Use | When |
|---|---|
| `records.insert`, `records.update`, `records.delete` | You want optimistic-concurrency checks, row history and live updates in open datasheets |
| `db.exec` | A bulk statement on workspace tables. It is not recorded in row history and cannot change the schema |

## Recipes {#recipes}

### Block a save from a form {#recipe-validate}

```lua
function before_update()
  local total = db.sum("OrderLines", "LineTotal", "OrderId = :id", { id = record.id })
  if total ~= db.NULL and total > 5000 and db.is_null(record.Approver) then
    ui.cancel("Orders over 5,000 need an approver.")
  end
end
```

### Fill a field from another table {#recipe-lookup}

```lua
function on_customer_change()
  local email = db.lookup("Customers", "Email", "id = :id", { id = record.Customer })
  record.Email = email
end
```

### Ask before doing something {#recipe-confirm}

```lua
function on_delete_all_click()
  ui.confirm("Delete all closed orders?", "do_delete")
end

function do_delete()
  if event.answer then
    local result = db.exec("DELETE FROM Orders WHERE Status = 'Closed'")
    notify.toast(result.changes .. " orders deleted")
  end
end
```

### Share code between scripts {#recipe-module}

Create a module named `util`:

```lua
local M = {}
function M.money(x) return string.format("%.2f", x) end
return M
```

Use it anywhere:

```lua
local util = require("util")
return util.money(12.5)
```

### Load a CSV from a web service every night {#recipe-job}

```lua
local res = fetch("https://data.example.com/stock.csv")
if not res.ok then error("download failed: " .. res.status) end

local st = staging.create({ columns = { "sku", "qty" } })
for row in csv.rows(res.body) do
  st:insert({ sku = row.sku, qty = tonumber(row.qty) })
end

local out = st:merge_into("Stock", { mode = "upsert", key = "sku" })
log.info("stock updated", { inserted = out.inserted, updated = out.updated })
artifacts.save("errors.csv", csv.encode(out.errors, { columns = { "row", "column", "message" } }))
```

The host `data.example.com` must be on the workspace's [egress allowlist](/docs/guide/jobs#egress). The `sku` column needs a unique index for `upsert`.

## Reference {#reference}

Every function, with parameters, errors and examples, is in the [Lua API reference](/docs/reference/lua-api). Modules most scripts use:

- [`db`](/docs/reference/lua-api#module-db) and [`records`](/docs/reference/lua-api#module-records) for data.
- [`ui`](/docs/reference/lua-api#module-ui), [`record`](/docs/reference/lua-api#module-record) and [`notify`](/docs/reference/lua-api#module-notify) in form events.
- [`fetch`](/docs/reference/lua-api#module-fetch), [`staging`](/docs/reference/lua-api#module-staging) and [`artifacts`](/docs/reference/lua-api#module-artifacts) in jobs.
- [`datetime`](/docs/reference/lua-api#module-datetime), [`json`](/docs/reference/lua-api#module-json), [`csv`](/docs/reference/lua-api#module-csv) and [`decimal`](/docs/reference/lua-api#module-decimal) anywhere.
