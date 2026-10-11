---
title: Tables
order: 10
summary: Define fields, link tables, edit records, resolve conflicts, filter and sort.
---

# Tables

A table holds your records. Builders and owners design tables, and see the **New table** and **Import…** buttons. Editors, builders and owners enter and change records. Viewers can read them.

## Create a table and add fields {#design}

1. In the **Tables** group of the navigation pane, choose **New table**. A design tab opens.
2. Type a name in the empty row at the bottom of the field grid. Each new name adds a field.
3. Choose the field's **Data type**.
4. Select the field to set its properties, such as **Required** or **Default value**, in the panel beside the grid.
5. Choose **Preview changes**, read the impact report, then choose **Apply**.

Nothing changes until you apply it. **Discard changes** drops your unapplied design changes.

Every table gets an AutoNumber field called `id` automatically. The database numbers it, so you cannot change its type, move it or delete it. You can rename it.

Each field has a field name (what people see) and an SQL name (used in queries and scripts). The SQL name starts as the field name with spaces replaced by `_`, so **Unit price** becomes `Unit_price`. The table name works the same way: the table **Order lines** is `Order_lines` in SQL. SQL names start with a letter, use only letters, digits and `_`, are at most 63 characters, and are unique within the table regardless of case.

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
| Location | One point on the Earth (latitude, longitude) | See [Locations and areas](#geo) |
| Area | A boundary: a polygon, with holes if needed, or several polygons | See [Locations and areas](#geo) |

## Locations and areas {#geo}

A **Location** field holds one point and an **Area** field holds a boundary such as a quarantine zone or a survey area. Both are stored as standard [GeoJSON](https://geojson.org) text, in WGS84 with the longitude first, so scripts, exports and the API can read them directly. Use an ordinary number field for anything about the position, such as GPS accuracy.

- **Where values come from.** The API and scripts accept GeoJSON. A location also accepts `{"lat": 51.50135, "lon": -0.14189}` and the text `51.50135, -0.14189`, and an import maps text such as `51.50135, -0.14189`, `POINT(-0.14189 51.50135)` or GeoJSON into a Location. A GeoJSON `Feature` is accepted anywhere a geometry is: its geometry is used.
- **What is stored.** Coordinates are rounded to 7 decimal places (about 1 cm). Rings are closed, outer rings are stored counter-clockwise and holes clockwise. A location may carry an altitude as a third coordinate; it is kept.
- **Limits.** Latitude must be from -90 to 90 and longitude from -180 to 180. Every ring needs at least 4 positions (the first repeated at the end). An area may have at most 10,000 positions and 1 MiB of GeoJSON. An area must be a Polygon or MultiPolygon.
- **The 180° meridian.** Areas that cross the 180° meridian are not supported and are rejected. Draw such an area as a MultiPolygon with one polygon on each side of the meridian.
- **No spatial index.** Spatial filters and joins scan the table, which is fine for tens of thousands of locations. A spatial join compares every location with every area, so keep both sides modest.
- **Changing the type.** A Location or Area can be changed to short or long text (a location becomes `lat, lon`, an area becomes its GeoJSON) and back, as long as the text is valid. Changing to or from any other type, or between Location and Area, is reported as a breaking change because the values cannot be converted.

### SQL functions {#geo-sql}

These functions work in queries, calculated fields and validation rules. Each returns empty (NULL) when an input is not valid, so one bad row cannot break a query.

| Function | Result |
|---|---|
| `geo_point(lat, lon)` | A location value |
| `geo_lat(point)`, `geo_lon(point)` | The coordinates of a location |
| `geo_distance_m(a, b)` | The distance in metres between two locations (great circle) |
| `geo_within(point, area)` | 1 when the location is inside the area, else 0. A location on the boundary is inside; holes are outside |
| `geo_area_m2(area)` | The area in square metres |
| `geo_centroid(area)` | The centre of the area, as a location. For a concave area it can lie outside |
| `geo_bbox(area)` | The bounding box as the JSON list `[min longitude, min latitude, max longitude, max latitude]` |

For example, a calculated field with result type **Location** and expression `geo_centroid(Fence)` gives each zone's centre, and `geo_distance_m(Spot, geo_point(51.5, -0.14)) < 500` is a validation rule or criterion.

A calculated field that uses these functions cannot be read by stock SQLite tools outside AbiData, because they do not know the functions. The stored values are plain GeoJSON text and read normally.

## Required fields, defaults and validation {#validation}

- **Required** rejects empty values. Saving a record without one fails with [REQUIRED_FIELD](/docs/reference/errors#required-field).
- **Default value** is a fixed SQL value such as `0` or `'New'`. It fills the field in new records.
- **Validation rule** is a SQL condition every row must satisfy, for example `Qty > 0`.
- **Validation text** is the message shown when the rule fails ([VALIDATION_FAILED](/docs/reference/errors#validation-failed)).

Tick **Hidden in the datasheet** to hide a field from the datasheet. Hidden fields still exist and still appear in queries.

An index speeds up searching and sorting. A unique index also rejects a second record with the same value.

- **Indexed** is a field property with three choices: **No**, **Yes (duplicates OK)** and **Yes (no duplicates)**. The last one makes the field unique. The ID field, and fields that belong to a multi-field index, do not offer it.
- **Indexes** (the button above the field grid) lists every index of the table, including ones over several fields. To add one, choose up to 16 fields in order, tick **Unique (no duplicate combinations)** if needed, and choose **Add index**. To remove an index, use its trash button.

Index changes are part of the design draft. They take effect through **Preview changes** and **Apply**, like any other change. If the table already holds duplicate values, a new unique index is reported as breaking and cannot be applied until the duplicates are fixed. Deleting a field also deletes every index that includes it. Once a unique index exists, a duplicate fails with [DUPLICATE_VALUE](/docs/reference/errors#duplicate-value). Unique text indexes compare case-insensitively, so `abc` and `ABC` are duplicates.

## Relationships and lookups {#lookups}

A Lookup field links each record to one record in another table (the parent).

1. Add a field and set its **Data type** to **Lookup**.
2. Under **Lookup settings**, choose the parent table in **Look up values from table**.
3. Choose the **Display field**: the parent field to show in place of the record number.
4. In **When the linked record is deleted**, choose what happens:
   - **Block the deletion (RESTRICT)**: the parent cannot be deleted while records link to it. This fails with [LINKED_RECORDS](/docs/reference/errors#linked-records).
   - **Delete this record too (CASCADE)**: deleting the parent deletes the linked records.
   - **Clear the link (SET NULL)**: deleting the parent empties the link.
5. Leave **Enforce referential integrity** ticked unless you are loading data that is not yet consistent.

In the datasheet, a lookup cell is a combobox that searches the parent table as you type.

## Calculated fields {#calculated}

A calculated field holds an SQL **Expression** over the other fields in the same table, such as `Quantity * Unit_price`. Choose a **Result type**: **Text**, **Integer**, **Number**, **Currency**, **Yes/No**, **Date** or **Date and time**. The expression may use built-in deterministic SQL functions only. Calculated fields cannot be edited in the datasheet.

## Enter and edit records {#editing}

Click a cell, or move with the arrow keys. Press `Enter` or `F2` to edit the cell, or start typing to replace its content.

- `Tab` and `Shift+Tab` move between cells.
- `Shift+Enter` saves the record. `Esc` discards your unsaved changes to the record.
- When you move to another row, AbiData saves the row you left. A half-edited record therefore does not reach other people until you leave it.
- The empty row at the bottom adds a new record. `Mod+Shift+N` jumps to it.
- To delete records, click a row header to select a record. `Shift+click` selects a range, and `Ctrl+click` (`⌘ click` on Mac) adds one. Then press `Delete` or choose **Delete record** in the toolbar. AbiData asks you to confirm.

See [Keyboard shortcuts](/docs/reference/shortcuts) for more.

Other people's changes appear live. The status bar shows **Updates: Live** while the connection works.

### Conflicts and the merge dialog {#editing-conflicts}

Every record has a version number. When you save, AbiData checks that the record has not changed since you started editing it. Nothing is ever overwritten silently.

If someone else saved the record first, AbiData compares field by field, using the values as they were when you started editing as the starting point:

- **Different fields.** If the other person changed fields you did not touch, or other fields than the ones you changed, your changes are saved on top of theirs. You see a short "Merged with changes made by someone else" message and no dialog.
- **Same value.** If the other person changed a field you also changed, but to the same value, there is nothing to decide.
- **Same field, different values.** Only then does the **Someone else changed this record** dialog open. It lists just those fields, with the value **Before** you started, **Theirs** and **Mine**. Choose **Mine** or **Theirs** for each, then choose **Save merged**. Your other, non-overlapping changes are saved along with your choices. **Discard mine** drops your edits and shows the other person's version.

Fields you did not change always keep the latest saved value. If several people keep saving the same record while you merge, AbiData tries a few times and then opens the dialog. If the other person deleted the record while you were editing it, your edits cannot be saved and you are told the record no longer exists. The underlying error is [CONCURRENT_MODIFICATION](/docs/reference/errors#concurrent-modification).

On a phone, saving from the record sheet works the same way.

## Filter and sort {#filters}

- **Filter records** searches the text columns for what you type. Press `Enter` to apply it.
- **Filter by selection** adds a condition based on the selected cell, for example "Status equals Open". Each condition appears as a chip with a remove button. **Clear filter** removes all of them.
- Click a column header to sort by it. Click again to reverse the order. The column menu, which has the column name on its button, also offers **Sort ascending**, **Sort descending** and **Clear sort**.
- To resize a column, drag its edge, or select its header and press `Alt+Left` or `Alt+Right`. Column widths and order are remembered in your browser, on this computer.

Filters and sorts apply only to your view. To save a selection for others, create a [query](/docs/guide/queries).

## History {#history}

In **Design**, tick **Track history of changes to rows**. From then on, every insert, update and delete made through the app is recorded with the user, the time, and the values before and after.

### Viewing a record's history {#view-history}

Anyone who can read the table can see a record's history, including the history of deleted records.

- **In the datasheet**, select a cell in the record and choose **History** in the toolbar.
- **In a form**, open the record and choose **History** in the form toolbar. It is available once the record is saved.

A panel opens with the changes, newest first. Each entry shows who made the change and when, and what changed. A new record lists its initial values and a deleted record lists its last values. An update lists only the fields that changed, as old value → new value. Fields that were later removed from the table appear as "(removed field)". Choose **Load older** to see earlier entries. Press `Esc` or choose **Close history** to close the panel.

If the table does not track history, the panel says so. A builder can turn tracking on in **Design**. Only changes made after tracking was turned on are recorded.

History is written by the datasheet, by forms and by the `records` module in scripts. Bulk imports and changes made with `db.exec` in a script are not recorded.

## Changing a design safely {#schema-changes}

**Preview changes** lists everything a change would affect: queries, forms, reports and scripts that use the field, rows that cannot be converted to a new type (with sample rows), and rows that break a new rule or index.

- Breaking issues are listed under **Breaking**. Constraint and foreign-key problems cannot be forced, so fix those rows first.
- Other breaking issues can be applied once you tick **I understand this may break the listed objects** and choose **Apply anyway**. Without that confirmation the change is refused with [SCHEMA_CHANGE_BLOCKED](/docs/reference/errors#schema-change-blocked).
- Warnings are listed separately and do not stop **Apply**.

Destructive changes take a restore point first. The impact report says "A restore point will be taken first". See [Backups](/docs/guide/backups).
