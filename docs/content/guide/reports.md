---
title: Reports
order: 40
summary: Design banded reports with grouping and totals, then print or save them as PDF.
---

# Reports

A report turns a table or saved query into paginated output with headers, groups and totals. Anyone who can read the data can run a report. Builders and owners design them.

## Create a report {#designer}

1. In the **Reports** group, choose **New report**.
2. Choose the record source (**Table** or **Saved query**), a name, paper size (**Letter**, **A4** or **Legal**) and **Portrait** or **Landscape**.
3. Add controls from the **Toolbox** and the **Field list**, and place them in bands.
4. Choose **Save**. Use **Preview** to see the report with data.

If the report needs input, base it on a [parameter query](/docs/guide/queries#parameters). Running the report asks for the values. The **Parameters** button on the viewer changes them.

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
- **Can grow**: the band becomes taller when its content needs more room.
- **Keep together**: the band is not split across pages.
- **Force new page**: the band starts on a new page.

Controls can be a label, a textbox (bound to a field or a calculation), a line or rectangle, and three special controls: **Page number**, **Page count** and **Now**.

## Grouping and sorting {#grouping}

Open **Groups and sorting** to add group levels and sort orders.

1. Add a group and choose the field or Lua expression to group on.
2. Choose **Ascending** or **Descending**.
3. Optionally set an **interval**. 0 groups on the whole value. For text, N groups on the first N characters. For numbers, N groups on multiples of N.
Each group adds a header band and a footer band. Put the group's title in the header and its totals in the footer.

A report with no groups prints records in the sort order you set.

## Totals {#totals}

Add an **Aggregate** to a textbox: choose a **Function** (**Count**, **Sum**, **Average**, **Minimum**, **Maximum**), the field, and a **Scope**: the group the band belongs to, or the whole report. **Running total** accumulates across records instead of resetting.

Calculated textboxes take a Lua expression over the record, such as `Qty * Price`. See [Scripts](/docs/guide/scripts#contexts): report scripts are read-only and have a 30 second budget for the whole run.

## Print and PDF {#printing}

Open the report and choose **Print**. The report is laid out at the exact paper size, so the browser print dialog shows the same pages you see on screen.

- To print, pick your printer.
- To make a PDF, choose **Save as PDF** as the destination in the print dialog. Set margins to **Default** or **None** and leave headers and footers off.

The paper size and orientation come from the report design. If the dialog shows a different size, set it to match.

## Limits {#limits}

- A report run stops at 200,000 detail records. Add criteria to the source query to reduce them.
- A run takes at most 30 seconds.
- At most two report runs per workspace and four across the server execute at the same time. Others fail with [WORKSPACE_BUSY](/docs/reference/errors#workspace-busy); try again shortly.
