---
title: Imports and exports
order: 70
summary: Bring CSV, Excel and JSON data into tables and export tables and queries.
---

# Imports and exports

Builders and owners import data. Export is available to every role that can see the table or query.

## Import data {#import}

The import wizard has four steps: **File**, **Preview and mapping**, **Check** and **Import**.

1. In the **Tables** group, choose **Import…** to create a new table, or open a table's menu and choose **Import into this table…**.
2. **File.** Choose a file of up to 100 MiB. Supported formats:
   - CSV, with the delimiter and encoding detected for you. A UTF-8 byte-order mark is accepted.
   - Excel (`.xlsx`). The first visible sheet is read.
   - JSON, either one array of objects or one object per line (NDJSON).

   Under **File options** you can set the format, delimiter and other details if the preview comes out wrong.
3. **Preview and mapping.** The first rows are shown and a type is guessed for each column from the first 1,000 rows. For a new table, name each field, adjust its type and mark it **Required**. For an existing table, map each source column to a field or choose **Skip this column**. Columns with the same name are matched automatically.
4. Choose a **Mode**:

   | Mode | What it does |
   |---|---|
   | Append | Adds every row as a new record |
   | Upsert | Updates the record whose key matches and adds the rest. The key must be a field with a unique index |
   | Replace | Deletes all records first, then adds the imported rows. You must tick a box to confirm |

   Choose a **Transaction** style:
   - **Chunked (default)** commits rows in batches of 2,000. If something fails you keep the batches already committed.
   - **All or nothing** is one transaction. Any failure rolls everything back. Avoid it for very large files.
5. **Check.** AbiData validates the rows and lists the first 100 problems with the row, column and reason. Fix the source file or the mapping and try again. Some problems, such as a duplicate in a unique field, are only found when rows are written.
6. **Import.** The import runs in the background. You can close the wizard and follow progress from the status bar. When it finishes, the result shows the number of rows **Inserted**, **Updated** and **Skipped**, and any errors.

A failed all-or-nothing import leaves your data unchanged, and a new table created by it is removed. A replace of more than 50,000 rows takes a [restore point](/docs/guide/backups#restore-points) first.

If rows are rejected the operation ends with [IMPORT_ROW_ERRORS](/docs/reference/errors#import-row-errors).

### Limits {#import-limits}

- Upload size: 100 MiB.
- At most 20 pending imports per workspace.
- A JSON object may have at most 256 keys.
- Uploaded files are kept for up to 24 hours and then deleted.

## Export data {#export}

1. Open the menu of a table or saved query, or use **Export…** on a datasheet.
2. Choose **CSV**, **Excel (.xlsx)** or **JSON** (an array of objects).
3. The export runs in the background. When it is done, the dialog offers a **Download** link.

Exports of saved queries are limited to 50,000 rows. Exported files are kept for 30 days.

CSV files protect you from spreadsheet formula injection: text that starts with `=`, `+`, `-`, `@`, a tab or a carriage return is prefixed with an apostrophe. Plain numbers are left alone. Column headers get the same treatment.

## From scripts {#scripts}

Jobs can read and write files too. See `csv.parse`, `csv.encode` and `staging` in the [Lua API reference](/docs/reference/lua-api#module-staging), and the nightly-load recipe in [Scripts](/docs/guide/scripts#recipe-job).
