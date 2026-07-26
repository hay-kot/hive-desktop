# Function

A **function** node runs author-trusted JavaScript against every message that reaches it. It has 1 input and up to 16 outputs (`outputs`, default 1).

## Fields

- `on_message` (required) — the body of `function on_message(msg, node, state) { ... }`. Return:
  - a single `msg` — goes out port 0
  - an array of `msg` — multiple messages, all on port 0 (when `outputs` is 1)
  - a port-indexed array (e.g. `[msg, null]`) — `array[i]` goes out port `i`, once `outputs` is more than 1
  - `null` — discard (reported, never silently dropped)
- `outputs` — 1 to 16, default 1.
- `timeout` — how long a single `on_message` call may run before it's terminated and the message is discarded as an error. 100ms to 60s, default 5s.

`on_message` is the whole lifecycle: there are no start or stop hooks. The node does no I/O, so a stop hook could only mutate state that is about to be discarded, and setup belongs inside `on_message` as lazy initialization:

```
state.counts ??= {};
```

## The msg shape

```
msg.Payload   // opaque — shape set by the source; reshape it toward the
              // canonical item contract (docs/decisions/0008) for feed rendering
msg.Key       // stable item identity (e.g. "colonyops/hive#2841")
msg.Topic     // "source:<source-id>"
msg.ID        // unique per log record
```

The message envelope is exactly these fields plus `Ts`, `SourceKind`, `SourceScope` and `OccurrenceKey`. Extra properties attached to `msg` itself are not carried to the next node — per-message data belongs in `msg.Payload`, which is opaque and passes through whole.

## Example

```
if (msg.Payload.state === "closed") return null;   // drop
msg.Payload.tag = "reviewed";
return msg;
```

## Behavior

Each node instance gets its own JavaScript VM, so a timeout only affects this node, never a sibling. `state` survives across messages for the lifetime of one Deploy, but is not durable across app restarts, and a node that times out is respawned with a fresh `state`.

A timeout interrupts the script cooperatively. Code that neither allocates nor returns to the interpreter — a tight empty loop — can outlive its interrupt; the node's message is still discarded as an error, and the abandoned evaluation consumes part of a fixed process-wide budget rather than blocking anything else.
