---
title: Scripts
order: 50
summary: Write Lua for forms, reports and jobs; know the contexts, time budgets and limits.
---

# Scripts

Scripts are written in Lua 5.1 and run in a sandbox on the server. Builders and owners create and edit them. The **Scripts** and **Jobs** groups are not shown to viewers and editors.

## Script kinds {#kinds}

| Kind | How you create it | Used by |
|---|---|---|
| Module | **New script** in the **Scripts** group, with **Kind** set to **Module** | Shared functions. Other scripts load it with `require("name")`. |
| Job | **New script**, with **Kind** set to **Job** | The code a [job](/docs/guide/jobs) runs |
| Form module | The **Module** panel of the form designer | Event handlers for one form |
| Report module | The **Module** panel of the report designer | Functions for one report's expressions |

Script Studio is the editor for scripts. It checks your code as you type. Syntax errors and unknown function names appear as markers and in the **Problems** list. Hover a function to see its documentation, with a link to the [Lua API reference](/docs/reference/lua-api). Save with `Mod+S`.

## Lua basics {#basics}

This runs on the **Orders** form and totals the order's lines in the Northwind sample.

```lua
local total = 0
for _, row in ipairs(db.query("SELECT Quantity, Unit_price FROM Order_lines WHERE Sales_order = :id", { id = record.id })) do
  total = total + row.Quantity * row.Unit_price
end
-- db.query returns currency as whole ten-thousandths, so divide by 10000.
local amount = decimal.new(total) / 10000
log.info("order total", { id = record.id, total = amount:tostring() })
return amount
```

Things that differ from what you may expect:

- **Names in scripts are SQL names.** A table or field is named with its display name, with spaces replaced by `_`. The sample table **Order lines** is `Order_lines`, and **Unit price** is `Unit_price`. See [Tables](/docs/guide/tables#design).
- **Empty fields are `db.NULL`, not `nil`.** Lua tables cannot hold `nil`. Test with `db.is_null(v)`, which is true for both.
- **Currency is `decimal` in records but a whole number in `db`.** Form fields and `records.get` give `decimal` values. `db.query`, `db.query_one`, `db.query_value`, `db.lookup` and the aggregates such as `db.sum` return the stored value, a whole number of ten-thousandths: `12.50` is `125000`. Divide by `10000`, as the example does, or read the value with `records.get`.
- **Dates and times are strings.** A date is `"2026-01-31"`; a timestamp is ISO 8601 UTC such as `"2026-03-09T05:30:00.000Z"`. They sort and compare correctly as text. Use `datetime` for calendar arithmetic.
- Integers beyond 2^53 are returned as strings.
- SQL uses named parameters (`:id`) filled from a table. Do not build SQL by joining strings.
- `print` writes to the run log.
- Besides a set of base functions (such as `pairs`, `ipairs`, `tostring`, `pcall` and `require`), only the `string`, `table` and `math` libraries are available. `os`, `io`, `debug`, `load`, `coroutine` and `package` are not.

## Contexts and budgets {#contexts}

A script runs in one of three contexts. The context decides which functions exist and how long the script may run.

| Context | Runs when | Time budget | `db.transaction` block | Longest string | Network | Writes |
|---|---|---|---|---|---|---|
| `ui_event` | A form event fires | 1 second (a workspace can be set from 250 ms to 5 seconds) | 250 ms | 1 MiB | No | Yes, if the user's role allows |
| `report` | A report runs | 30 seconds per run | not available | 1 MiB | No | No |
| `job` | A job runs | The job's timeout, up to 3,600 s (default 300 s) | 2 s | 16 MiB | `fetch` to allowed hosts | Yes |

Waiting for a free slot counts against the budget. If a script is too slow, you get [SCRIPT_TIMEOUT](/docs/reference/errors#script-timeout). If it builds a string that is too long, uses a runaway pattern, or the server runs low on memory, you get [SCRIPT_LIMIT_EXCEEDED](/docs/reference/errors#script-limit-exceeded). Any other failure is [SCRIPT_ERROR](/docs/reference/errors#script-error), with the line number.

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

This runs on the **Orders** form. A shipped order must have a customer.

```lua
function before_update()
  if record.Status == "Shipped" and db.is_null(record.Customer) then
    ui.cancel("A shipped order needs a customer.")
  end
end
```

### Fill a field from another table {#recipe-lookup}

This runs on the **Orders** form, when the **Customer** lookup changes. It copies the customer's city into **Notes**.

```lua
function on_customer_change()
  local city = db.lookup("Customers", "City", "id = :id", { id = record.get("Customer") })
  if not db.is_null(city) then
    record.set("Notes", "Ship to " .. city)
  end
end
```

### Ask before doing something {#recipe-confirm}

This runs on the **Products** form. The button asks first, then marks every product with no stock as discontinued.

```lua
function on_discontinue_click()
  ui.confirm("Discontinue every product with no stock?", "do_discontinue")
end

function do_discontinue()
  if event.answer then
    local result = db.exec("UPDATE Products SET Discontinued = 1 WHERE In_stock = 0")
    notify.toast(result.changes .. " products discontinued")
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

This job updates stock levels in **Products** from a CSV whose header is `Product,In_stock`. Staging column names must match the table's field names; `In_stock` is the SQL name of **In stock**.

```lua
local res = fetch("https://data.example.com/stock.csv")
if not res.ok then error("download failed: " .. res.status) end

local st = staging.create({ columns = { "Product", "In_stock" } })
for row in csv.rows(res.body) do
  st:insert({ Product = row.Product, In_stock = tonumber(row.In_stock) })
end

local out = st:merge_into("Products", { mode = "upsert", key = "Product" })
log.info("stock updated", { inserted = out.inserted, updated = out.updated })
artifacts.save("errors.csv", csv.encode(out.errors, { columns = { "row", "column", "code", "message" } }))
```

The host `data.example.com` must be on the platform's [egress allowlist](/docs/guide/jobs#egress), which a platform administrator manages. The `Product` field needs a unique index for `upsert`. The Northwind sample does not have one, so first set **Indexed** to **Yes (no duplicates)** on **Product** in the **Products** design tab (see [Tables](/docs/guide/tables#validation)).

## Reference {#reference}

Every function, with parameters, errors and examples, is in the [Lua API reference](/docs/reference/lua-api). Modules most scripts use:

- [`db`](/docs/reference/lua-api#module-db) and [`records`](/docs/reference/lua-api#module-records) for data.
- [`ui`](/docs/reference/lua-api#module-ui), [`record`](/docs/reference/lua-api#module-record) and [`notify`](/docs/reference/lua-api#module-notify) in form events.
- [`fetch`](/docs/reference/lua-api#module-fetch), [`staging`](/docs/reference/lua-api#module-staging) and [`artifacts`](/docs/reference/lua-api#module-artifacts) in jobs.
- [`datetime`](/docs/reference/lua-api#module-datetime), [`json`](/docs/reference/lua-api#module-json), [`csv`](/docs/reference/lua-api#module-csv) and [`decimal`](/docs/reference/lua-api#module-decimal) anywhere.
