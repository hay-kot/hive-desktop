# Clipboard

A **clipboard** action renders a template over the triggering item and puts the
result on the clipboard. Use it for "get me a ready-to-paste command" — a
`gh pr checkout`, a branch name, a link — without shelling out to `pbcopy`.

## Fields

- `text_template` (required) — the text placed on the clipboard.

## Templates

`text_template` is a Go `text/template` rendered over the triggering message
(see "Template data" above), the same context a shell action's
`command_template` renders over. The `shq` helper is available, though clipboard
text is not run through a shell, so quoting is rarely needed:

```
text_template: "gh pr checkout {{ .Payload.num }} -R {{ .Payload.repo }}"
```

## Detail pane only

A clipboard action is offered on an item's detail pane; it is not runnable from
a flow, which has no clipboard to write to. It leaves no durable command record:
copying the same item again just re-copies, with no rerun prompt.
