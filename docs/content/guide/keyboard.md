---
title: Using the keyboard
order: 100
summary: Work without a mouse, and find the full list of shortcuts.
---

# Using the keyboard

You can do most work from the keyboard. The full list of shortcuts, with Mac and Windows/Linux keys, is in [Keyboard shortcuts](/docs/reference/shortcuts). This page explains how the keys behave in each area. `Mod` means `⌘` (Command) on Mac and `Ctrl` on Windows and Linux.

## The essentials {#essentials}

- `Mod+K` opens the command palette (see [The command palette](#palette)). Type to search, use the arrow keys to move, `Enter` to run and `Esc` to close.
- `?` opens this documentation when you are not typing in a field. The help button in the top bar, labelled **Open documentation**, does the same.
- `Mod+S` saves the design you are editing: a table design, a query, a form, a report, a script or a job.
- `Shift+F10`, or the context-menu key, opens the menu you would get from a right-click. This works in the navigation pane, document tabs, datasheets, query results and designers.
- `Esc` closes dialogs and panels. In the documentation panel, the first `Esc` clears the search and the next one closes the panel.

AbiData never uses `F1` or `Ctrl`/`Cmd` with `N`, `T`, `W`, `E`, `L` or `P`, so your browser's own shortcuts keep working.

## The command palette {#palette}

The palette is the fast way to do almost anything. With no text it lists the objects you opened lately (**Recent**) and the main actions. Type a few letters of a name or an action to narrow the list; letters need not be next to each other, so `cstmr` finds Customers.

- **Objects.** Inside a workspace, every table, query, form, report, script and job is listed under its own heading with its icon. `Enter` opens it, the same as clicking it in the navigation pane. A heading shows up to five matches; choose **Show N more** to see the rest.
- **Actions.** New table, New query, SQL console, New form, New report, New script, New job, Import data… and Workspace settings appear only when your role allows them in the interface. Switch workspace, theme, documentation, keyboard shortcuts, Change password… and Sign out are always there. Platform administrators also get Admin console and one entry for each of its tabs.
- **Phones.** The palette fills the width of the screen and its rows are large enough to tap. The search button in the top bar opens it.

## Moving between areas {#areas}

`Tab` and `Shift+Tab` move the focus to the next or previous control, in the order they appear on the screen.

In the tab strip, press `Delete` to close the focused tab. If the tab has unsaved changes, AbiData asks you to confirm before it discards them.

## Datasheets {#datasheets}

The datasheet is a grid.

- Arrow keys move between cells and records. `Tab` and `Shift+Tab` move to the next or previous cell. `Page Up` and `Page Down` move one page. `Mod+Home` and `Mod+End` go to the first and last record.
- To leave the grid, go to the first cell with `Mod+Home` and press `Shift+Tab` (back to the toolbar), or go to the last cell with `Mod+End` and press `Tab` (on to the rest of the page). In between, `Tab` stays inside the grid and moves from cell to cell.
- `Enter` or `F2` edits the current cell. Typing a character starts an edit with that character. In a long text cell, `Alt+Enter` adds a line break.
- `Shift+Enter` saves the record. `Esc` cancels the edit, or discards your unsaved changes to the record.
- `Space` toggles a Yes/No cell. `Shift+Space` selects or deselects the current record.
- `Delete` deletes the selected records, after you confirm.
- `Mod+Shift+N` goes to the new record row. `Mod+F` moves to the filter box. Elsewhere, `Mod+F` is your browser's find.
- With a column header focused, `Alt+Left` and `Alt+Right` make the column narrower or wider.

See [Tables](/docs/guide/tables#editing) for editing and [conflicts](/docs/guide/tables#editing-conflicts).

## Forms {#forms}

- `Alt+Up` and `Alt+Down` move to the previous and next record.
- `Mod+Shift+N` starts a new record.
- `Shift+Enter` saves the record. `Esc` discards unsaved changes.

## Designers {#designers}

In the form and report designers, every control can be reached without a mouse.

- `Tab` and `Shift+Tab` move the focus between controls.
- `Enter` selects only the focused control. `Space` adds or removes it from the selection.
- The arrow keys move the selected controls by one grid step. `Shift` with an arrow key resizes them.
- `Mod+Arrow` moves the focus without selecting.
- `Mod+A` selects all controls, `Mod+D` duplicates the selection, and `Delete` or `Backspace` deletes it.
- `Esc` clears the selection.

## Lists, grids and viewers {#lists}

- **Command palette**: the arrow keys move the highlight, `Page Up` and `Page Down` move a page at a time, and `Enter` runs the highlighted result. `Home` and `End` go to the first and last result once the text cursor is already at the start or end of what you typed.
- **Documentation contents**: the arrow keys move the highlight, `Enter` opens the item, and `Home` and `End` go to the first and last item.
- **Query results**: arrow keys move between cells, `Page Up` and `Page Down` move one page, and `Home` and `End` go to the first and last row.
- **Script Studio problems list**: the arrow keys and `Home` and `End` move through the problems, and `Enter` jumps to the line of the selected one.
- **Report viewer**: `Page Up` and `Page Down` move between pages, and `Home` and `End` go to the first and last page.
- **Panel dividers**: when a divider between panes has focus, the arrow keys move it. Hold `Shift` for bigger steps.

## Screen readers {#screen-readers}

Controls have labels, and changes such as the number of search results are announced politely. If something is not reachable by keyboard or is not announced, that is a bug worth reporting.
