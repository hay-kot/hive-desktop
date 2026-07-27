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

## Firing once, not on every update

An item changes many times over its life — a PR you are watching collects
comments, commits and reviews — and each change is a distinct event. By
default a notify node fires **once per item** and stays quiet through that
later churn: the notification you asked for is the item arriving, not every
subsequent nudge to it.

Set `dedup` to control what counts as "again". It is a template rendered over
the message, and its value is the key delivery deduplicates on — so the node
fires once per distinct value and again only when that value changes:

```
dedup: "{{ .Payload.state }}"
```

fires when a PR's state changes but not on its comment traffic. `dedup`
matches on a *value*, not a transition: a value returning to one already seen
(open → closed → open) does not fire again.

## Clicking a notification

Clicking the banner focuses Hive and selects the item that triggered it. An
item Hive no longer holds (or a message no source produced) still notifies —
the click just raises the window.

## What stops it from being noisy

- **Dedup.** Delivery deduplicates on the item id by default, or on the
  `dedup` value when set (see *Firing once, not on every update*), so an item
  that keeps changing interrupts once rather than on every update. A message
  with no id and no `dedup` falls back to a digest of its payload, so identical
  payloads still collapse.
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
