---
title: Tables
order: 10
summary: Define fields, link tables, edit records, resolve conflicts, filter and sort.
---

# Tables

A table holds your records. Builders and owners design tables; editors, builders and owners enter data; viewers can read.

## Create a table and add fields {#design}

1. In the navigation pane, choose **New table**. A design tab opens.
2. Add fields in the grid. Give each a name and a data type.
3. Select a field to set its properties in the panel beside the grid.
4. Choose **Preview changes**, read the impact report, then apply.

Every table gets an AutoNumber field named `id` automatically. It is the primary key and you cannot change it.

Each field has a display name (what people see) and a SQL name (used in queries and expressions). The SQL name starts as the display name with spaces replaced by `_`. SQL names start with a letter, use only letters, digits and `_`, are at most 63 characters, and are unique within the table regardless of case.

## Field types {#field-types}

| Type | Stores | Notes |
|---|---|---|
| AutoNumber | Whole number | One per table, named `id`, assigned by the database |
| Short text | Text | Field size 1 to 65,535 characters, default 255 |
| Long text | Text | No length limit beyond workspace quotas |
| Integer | Whole number | Values beyond 2^53 reach scripts and JSON as strings |
| Number | Floating point | |
| Currency | Exact decimal | Four decimal places; scripts receive a `decimal` value |
| Yes/No | True or false | Never empty; defaults to No |
| Date | `YYYY-MM-DD` | |
| Date and time | UTC timestamp | |
| Lookup | A link to a record in another table | See [Relationships and lookups](#lookups) |
| Hyperlink | Text starting with `http://` or `https://` | |
| Calculated | A value computed from other fields | Read-only; see [Calculated fields](#calculated) |

## Required fields, defaults and validation {#validation}

- **Required** rejects empty values. Saving a record without one fails with [REQUIRED_FIELD](/docs/reference/errors#required-field).
- **Default value** is a fixed SQL value such as `0` or `'New'`. It fills the field in new records.
- **Validation rule** is a SQL condition every row must satisfy, for example `Qty > 0`.
- **Validation text** is the message shown when the rule fails ([VALIDATION_FAILED](/docs/reference/errors#validation-failed)).

You can mark a field **Hidden in the datasheet**. Hidden fields still exist and still appear in queries.

An index speeds up searching and sorting, and a unique index also rejects a second record with the same value.

- **Indexed** (a field property) is **No**, **Yes (duplicates OK)** or **Yes (no duplicates)**. The last one makes the field unique. The system ID field and fields that are part of a multi-field index do not offer it.
- **Indexes** (button above the field grid) lists every index of the table, including ones over several fields. To add one, choose 1 to 16 fields in order and tick **Unique (no duplicate combinations)**; use the trash button to delete one.

Index changes are part of the design draft: they take effect through **Preview changes** and **Apply** like any other change. If the table already holds duplicate values, creating a unique index is reported as breaking in the preview and cannot be applied until the duplicates are fixed. Deleting a field also deletes every index that includes it. Once a unique index exists, a duplicate fails with [DUPLICATE_VALUE](/docs/reference/errors#duplicate-value). Unique text indexes compare case-insensitively, so `abc` and `ABC` are duplicates.

## Relationships and lookups {#lookups}

A Lookup field links each record to one record in another table (the parent).

1. Add a field and set its type to **Lookup**.
2. Under **Look up values from table**, choose the parent table.
3. Choose the field to show in place of the record number.
4. Choose what happens when a linked record is deleted:
   - **Block the deletion (RESTRICT)**: the parent cannot be deleted while records link to it. This fails with [LINKED_RECORDS](/docs/reference/errors#linked-records).
   - **Delete this record too (CASCADE)**: deleting the parent deletes the linked records.
   - **Clear the link (SET NULL)**: deleting the parent empties the link.
5. Leave **Enforce referential integrity** on unless you are loading data that is not yet consistent.

In the datasheet, a lookup cell is a combobox that searches the parent table as you type.

## Calculated fields {#calculated}

A calculated field holds a SQL expression over the other fields in the same table, such as `Qty * Price`. Choose a result type for it. The expression may use built-in deterministic SQL functions only. Calculated fields cannot be edited in the datasheet.

## Enter and edit records {#editing}

Click a cell or move with the arrow keys. Press `Enter` or `F2` to edit. Type to replace the content.

- `Tab` and `Shift+Tab` move between cells.
- `Shift+Enter` saves the record.
- `Esc` discards your unsaved changes to the record.
- The empty row at the bottom adds a new record.
- `Mod+Shift+N` jumps to the new record row.
- To delete, select rows with `Shift+Space` or the row selectors, then press `Delete` or choose **Delete record**. You confirm first.

See [Keyboard shortcuts](/docs/reference/shortcuts) for more.

A record is saved as a whole when you leave it, so a half-edited record does not reach other users until you finish. Other people's changes appear live: the status bar shows **Updates: Live** when the connection is working.

### Conflicts and the merge dialog {#editing-conflicts}

Every record has a version number. When you save, AbiData checks that the record has not changed since you opened it. If someone else saved first, nothing is overwritten and the **Someone else changed this record** dialog opens.

For each field you changed, choose **Mine** or **Theirs**, then choose **Save merged**. **Discard mine** drops your edits and shows the other person's version. The underlying error is [CONCURRENT_MODIFICATION](/docs/reference/errors#concurrent-modification).

Fields you did not change always keep the latest saved value.

## Filter and sort {#filters}

- **Filter records** searches the text columns for what you type. Press `Enter` to apply it.
- **Filter by selection** adds a condition from the selected cell, for example "Status equals Open". Each condition appears as a chip you can remove. **Clear filter** removes all of them.
- Click a column header to sort. Click again to reverse. The header menu also has **Clear sort**.
- Drag a column edge, or press `Alt+Left` / `Alt+Right` on a header, to resize it. Column widths and order are remembered in your browser.

Filters and sorts are for your view only. To save a selection for others, create a [query](/docs/guide/queries).

## History {#history}

In **Design**, turn on **Track history of changes to rows**. From then on, every insert, update and delete through the app is recorded with the user, time and the values before and after.

History is written by the row API (the datasheet), forms and the `records` module in scripts. Bulk imports and changes made with `db.exec` in a script are not recorded.

## Changing a design safely {#schema-changes}

Preview changes lists everything a change would affect: queries, forms, reports and scripts that use the field, rows that cannot be converted to a new type (with samples), and rows that break a new rule. Warnings can be accepted. Breaking changes are refused with [SCHEMA_CHANGE_BLOCKED](/docs/reference/errors#schema-change-blocked) until you fix the listed items or confirm.

Destructive changes take a restore point first. See [Backups](/docs/guide/backups).
