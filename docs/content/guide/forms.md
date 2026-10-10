---
title: Forms
order: 30
summary: Design data-entry forms, attach Lua event handlers, add calculated controls and subforms.
---

# Forms

A form is a screen for viewing and entering records. Builders and owners design forms. Editors, builders and owners use them to change data; viewers can open them read-only.

## Create a form {#designer}

1. In the **Forms** group, choose **New form**.
2. Pick the record source: a **Table**, a **Saved query**, or **None (unbound form)** for a screen that only runs scripts.
3. Pick the default view: **Single form**, **Continuous forms** or **Datasheet**.
4. Add controls from the **Toolbox**, or add a field from the **Field list**.
5. Select a control to edit its position, size and binding in the property sheet. Controls snap to an 8 px grid.
6. Choose **Save** (`Mod+S`).

The designer has three panels: **Layout**, **Module** (the form's Lua source) and **Preview**, which shows the form with sample data.

Form properties include **Allow edits**, **Allow additions** and **Allow deletions**, and the size of the header, detail and footer sections. These settings shape the screen. They are not a security boundary: anyone with the editor role can change the underlying table in other ways. Use [roles](/docs/guide/members-and-roles) for access control.

## Control types {#controls}

| Control | Use |
|---|---|
| Label | Fixed text |
| Textbox | Shows or edits a field, or a calculation |
| Combobox | Picks a value from a table or saved query |
| Checkbox | Yes/No field |
| Button | Runs a Lua handler when clicked |
| Line, Rectangle | Decoration |
| Subform | Embeds another form |

## Events {#events}

Event handlers are functions in the form's module. In the property sheet, type the function name next to the event.

Form events:

- **load**: the form opens.
- **current**: the current record changes.
- **before_insert**, **before_update**, **before_delete**: run before the change. A handler can stop it with `ui.cancel("message")`.
- **after_update**, **after_delete**: run after the change was saved.
- **close**: the form closes.

Control events: **click**, **change** (debounced 300 ms, only for controls that name a handler), **before_update** and **after_update**.

Example module:

```lua
function before_update()
  if record.Qty <= 0 then
    ui.cancel("Quantity must be positive.")
  end
end

function on_ship_click()
  ui.confirm("Ship this order?", "do_ship")
end

function do_ship()
  if event.answer then
    records.update("Orders", record.id, { Status = "Shipped" })
    notify.toast("Order shipped")
  end
end
```

Form handlers run with a 1 second time budget and may write data when the user's role allows it. See [Scripts](/docs/guide/scripts#contexts) for the limits and the [Lua API reference](/docs/reference/lua-api#module-ui) for `ui`, `record` and `notify`.

When a handler fails, the user sees [SCRIPT_ERROR](/docs/reference/errors#script-error). If it runs out of time, they see [SCRIPT_TIMEOUT](/docs/reference/errors#script-timeout).

### Saving {#saving}

Edits stay in a buffer in the browser until saved (`Shift+Enter`, or moving to another record). On save, the server runs `before_update`, writes the record, then runs `after_update`. If `before_update` cancels, nothing is written and the message is shown. If someone else changed the record first, you get the same field-by-field merge dialog as in the datasheet: see [Conflicts](/docs/guide/tables#editing-conflicts).

`Esc` discards the unsaved changes. The form warns you before you leave with unsaved changes.

### Confirm {#confirm}

`ui.confirm(message, continuation)` pauses the handler and asks the user a yes/no question. When they answer, the server calls the function named by `continuation`, and `event.answer` is `true` or `false`. A confirmation is tied to the user, the form and the moment it was asked, so it cannot be replayed later.

## Calculated controls {#calculated}

Set a textbox's **Data** to **Calculation** and enter one Lua expression over the record, for example:

```lua
record.Qty * record.Price
```

The server calculates these values when the form loads, when the current record changes and after each save, so they always match the saved data. Calculated controls are read-only.

## Subforms {#subforms}

A subform embeds another form, usually showing the child records of the current record, such as the lines of an order.

1. Add a **Subform** control.
2. Choose the form to embed.
3. Set **Link master field** to the field on this form and **Link child field** to the matching field on the embedded form.

The subform shows only records whose child field equals the master field, and new records created in it get that value automatically.

## Move around {#navigation}

- `Alt+Up` and `Alt+Down` move to the previous or next record.
- `Mod+Shift+N` starts a new record.
- `Shift+Enter` saves, `Esc` discards.

See [Keyboard shortcuts](/docs/reference/shortcuts).
