# Notify

A **notify** node is a terminal (one input, no outputs). Every arriving
message creates a durable `output_command` that the backend delivers as a
native system notification. It is how a flow says "this one is worth
interrupting me for" — routing, rather than an app-wide on/off switch.

## Templates

`title` and `body` are Go templates rendered over the arriving message, the
same way an action's `prompt_template` is:

```
title: "{{ .Payload.repo }} needs review"
body:  "{{ .Payload.title }}"
```

`.Payload` is the item, `.Key` its source id. A title that renders blank
fails the command rather than sending a nameless banner.

## Clicking a notification

Clicking the banner focuses Hive and selects the item that triggered it. An
item Hive no longer holds (or a message no source produced) still notifies —
the click just raises the window.

## What stops it from being noisy

- **Dedup.** Commands deduplicate on the message's occurrence key, which
  changes only when something meaningful about the item changed. A source
  re-emitting an unchanged item on every poll notifies once, not once per
  tick. A message with no occurrence key falls back to a digest of its
  payload, so identical payloads still collapse.
- **Cooldown.** One item can only interrupt you once every 5 minutes per
  node, however often it genuinely changes.
- **Staleness.** A notification queued more than 10 minutes before it could
  be delivered — the app was closed, or the queue was backed up — is dropped
  instead of arriving late.
- **Replay.** Recomputing a flow on startup or deploy never re-sends
  anything: replay commits feed memberships only, and snapshot messages are
  dropped before they reach this node.

## Settings always win

Delivery is checked against the app's notification settings immediately
before each send, so turning notifications (or system notifications) off
silences every flow at once. `sound: false` can silence this node, but it
cannot make a notification audible when the global sound setting is off.

`severity` selects the native interruption level: `info` (the default) and
`success` are ordinary banners, while `warning` and `error` are marked
time-sensitive.
