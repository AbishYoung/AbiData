---
title: Forms
order: 30
summary: Design data-entry forms, attach Lua event handlers, add calculated controls and subforms.
---

# Forms

A form is a screen for viewing and entering records. Builders and owners design forms. Editors, builders and owners use them to change data. Viewers can open them, but read-only.

## Create a form {#designer}

1. In the **Forms** group, choose **New form**.
2. Enter a **Form name**. Under **Record source type**, choose **Table**, **Saved query** or **None (unbound form)**, then choose the **Record source**. An unbound form only runs scripts.
3. Under **Default view**, choose **Single form**, **Continuous forms** or **Datasheet**.
4. Add controls from the **Toolbox**, or drag a field from the **Field list** onto the form.
5. Select a control to set its position, size, data and events in the property sheet. Controls snap to an 8 px grid.
6. Choose **Save** (`Mod+S`).

The designer has three panels, selected above the canvas: **Layout**, **Module** (the form's Lua source) and **Preview**, which shows the form with sample data.

The form's **Permissions** group has **Allow edits**, **Allow additions** and **Allow deletions**. The **Size** group sets the form width and the header, detail and footer heights. These settings shape the screen. They are not a security boundary: anyone with the editor role can change the underlying table in other ways. Use [roles](/docs/guide/members-and-roles) for access control.

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

The property sheet for a control has these groups: **General** (name, caption and section), **Position and size**, **Behavior** (**Visible**, **Enabled**, **Locked**), **Data**, and **Events**. The **Data** group sets **Control source** to **None (unbound)**, **Bound field** or **Calculation**.

## Events {#events}

Event handlers are functions in the form's module. In the property sheet, type the function name next to the event.

Form events:

- **load**: the form opens.
- **current**: the current record changes.
- **before_insert**, **before_update**, **before_delete**: run before the change. A handler can stop it with `ui.cancel("message")`.
- **after_update**, **after_delete**: run after the change was saved.
- **close**: the form closes.

Control events are labelled **On click**, **Before update**, **After update** and **On change**. Labels and buttons have only **On click**. Textboxes, comboboxes and checkboxes have all four. **On change** runs after the user stops typing for 300 ms.

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

Form handlers run with a 1 second time budget and may write data when the user's role allows it. See [Scripts](/docs/guide/scripts#contexts) for the limits, and the [Lua API reference](/docs/reference/lua-api#module-ui) for `ui`, `record` and `notify`.

When a handler fails, the user sees [SCRIPT_ERROR](/docs/reference/errors#script-error). If it runs out of time, they see [SCRIPT_TIMEOUT](/docs/reference/errors#script-timeout).

### Saving {#saving}

Edits stay in the form until you save them: press `Shift+Enter`, or move to another record. The toolbar shows **Unsaved changes** while you have edits. On save, the server runs `before_update`, writes the record, then runs `after_update`. If `before_update` cancels, nothing is written and the message is shown.

If someone else changed the record first, the form shows **This record was changed by someone else while you were editing.** Choose **Keep my changes** to keep your edits, or **Use their version** to drop them and see the saved record. Unlike the datasheet, a form does not show the field-by-field merge dialog. For that, see [Conflicts](/docs/guide/tables#editing-conflicts).

`Esc` discards the unsaved changes. The form warns you before you leave with unsaved changes.

### Confirm {#confirm}

`ui.confirm(message, continuation)` pauses the handler and asks the user a yes/no question. When they answer, the server calls the function named by `continuation`, and `event.answer` is `true` or `false`. A confirmation is tied to the user, the form and the moment it was asked, so it cannot be replayed later.

## Calculated controls {#calculated}

Set a textbox's **Control source** to **Calculation**, and enter one Lua expression in **Lua expression** over the record, for example:

```lua
record.Qty * record.Price
```

The server calculates these values when the form loads, when the current record changes and after each save, so they always match the saved data. Calculated controls are read-only.

## Subforms {#subforms}

A subform embeds another form, usually to show the child records of the current record, such as the lines of an order.

1. Add a **Subform** control.
2. In the **Subform** group, choose the **Form** to embed.
3. Set **Link master field** to the field on this form, and **Link child field** to the matching field on the embedded form.

The subform shows only records whose child field equals the master field. New records created in it get that value automatically.

## Move around {#navigation}

- `Alt+Up` and `Alt+Down` move to the previous or next record.
- `Mod+Shift+N` starts a new record.
- `Shift+Enter` saves, and `Esc` discards unsaved changes.

See [Keyboard shortcuts](/docs/reference/shortcuts).
