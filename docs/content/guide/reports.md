---
title: Reports
order: 40
summary: Design banded reports with grouping and totals, then print or save them as PDF.
---

# Reports

A report turns a table or saved query into paginated output with headers, groups and totals. Anyone who can read the data can run a report. Builders and owners design them.

## Create a report {#designer}

1. In the **Reports** group, choose **New report**.
2. Enter a **Report name** and choose the **Record source**: a table or a saved query.
3. In the page settings, choose the **Paper** (**Letter**, **A4** or **Legal**), the **Orientation** (**Portrait** or **Landscape**) and the margins.
4. Choose a band, then add controls to it from the **Toolbox**, or drag fields from the **Field list**.
5. Choose **Save**. Use the **Preview** panel to see the report with data.

If the report needs input, base it on a [parameter query](/docs/guide/queries#parameters). Running the report asks for the values. The **Parameters** button in the viewer changes them later.

## Bands {#bands}

A report is divided into horizontal bands. Each band prints at a fixed place:

| Band | Prints |
|---|---|
| Report header | Once, at the start of the report |
| Page header | At the top of every page |
| Group header *n* | Before each group at level *n* |
| Detail | Once per record |
| Group footer *n* | After each group at level *n*, usually totals |
| Page footer | At the bottom of every page |
| Report footer | Once, at the end of the report |

Each band has these settings:

- **Band height**.
- **Can grow**: the band becomes taller when wrapped text needs more room.
- **Keep together**: the band is not split across pages.
- **Force new page**: the band starts on a new page.
- **Visible**.

Controls can be a label, a textbox, a line, a rectangle, and three special controls: **Page number**, **Page count** and **Now**. A textbox's **Control source** is **None (caption text)**, **Field**, **Calculation** or **Aggregate**.

## Grouping and sorting {#grouping}

Open the **Groups and sorting** panel to add group levels and sort orders.

1. Choose **Add group**.
2. Choose the field to group on, or **Expression (Lua)** to group on a Lua expression.
3. Choose **Ascending** or **Descending**.
4. Optionally set the group's **interval**. 0 groups on the whole value. For text, N groups on the first N characters. For numbers, N groups on multiples of N.

Each group adds a header band and a footer band. Put the group's title in the header and its totals in the footer.

To sort the detail rows, choose **Add sort** under **Sort order**, pick a **Sort n field**, and tick **Sort n descending** if needed. Without groups, the records print in this sort order.

## Totals {#totals}

Set a textbox's **Control source** to **Aggregate**, then choose:

- **Function**: **Sum**, **Average**, **Count**, **Minimum** or **Maximum**.
- **Aggregate field**: the field to add up. For **Count**, leave it on **All rows**.
- **Scope**: **Group** totals each group, and **Report** totals the whole report.

Tick **Running total** to accumulate across records instead of starting again for each group.

For a value that is not an aggregate, set **Control source** to **Calculation** and enter a Lua expression over the record, such as `record.Qty * record.Price`. Functions from the report module are available there too. Report scripts are read-only and have a 30 second budget for the whole run. See [Scripts](/docs/guide/scripts#contexts).

## Print and PDF {#printing}

Open the report and choose **Print**. The report is laid out at its exact paper size, so the print dialog shows the same pages you see on screen.

- To print, pick your printer.
- To make a PDF, choose **Save as PDF** as the destination in the print dialog. Browsers differ in their print options. If the layout is off, set margins to **Default** or **None** and turn headers and footers off.

The paper size and orientation come from the report design. If the dialog shows a different size, set it to match.

## Limits {#limits}

- A report run stops at 200,000 detail records. Add criteria to the source query to reduce them.
- A run takes at most 30 seconds.
- At most two report runs per workspace and four across the server execute at the same time. Others fail with [WORKSPACE_BUSY](/docs/reference/errors#workspace-busy). Try again shortly.
