# Shell

A **shell** action runs an author-trusted command line via `sh -c`. Use it to
reach anything the desktop app has no first-class integration with — a CLI, a
script, a `curl` to an internal service.

## Fields

- `command_template` (required) — the command line to run.
- `cwd` — working directory; defaults to the desktop process's own.
- `timeout` — a duration string like `"30s"` bounding the run. Must be quoted:
  a bare number is a hard error, not seconds. Omit for no deadline beyond the
  invoking context's.
- `env` — a map of extra environment variables for the command.

## Templates and quoting

`command_template` is a Go `text/template` rendered over the triggering message
(see "Template data" above). **Pipe every interpolated value through the `shq`
helper** — it shell-quotes the value so a title containing spaces, quotes, or
`;` cannot break out of its argument:

```
command_template: 'notify-send {{ .Payload.title | shq }} {{ .Payload.url | shq }}'
```

Failed runs keep bounded stdout/stderr diagnostics on the durable command
record, readable from the activity view.
