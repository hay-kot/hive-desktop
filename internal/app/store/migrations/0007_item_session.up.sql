-- The inbox item a queued action ran against. The payload alone cannot answer
-- it: an action command's dedup key is the message's occurrence key, not the
-- item's external id, so a command that outlives a restart has no way back to
-- the row that produced it. The notify sink already carries the same four
-- fields inside its own payload envelope; an action's payload is the item and
-- cannot be wrapped without breaking every template, so they become columns.
-- Empty means the command has no inbox origin (a manual invocation from a
-- surface that has no item behind it).
ALTER TABLE output_command ADD COLUMN profile_id TEXT NOT NULL DEFAULT '';
ALTER TABLE output_command ADD COLUMN source_kind TEXT NOT NULL DEFAULT '';
ALTER TABLE output_command ADD COLUMN source_scope TEXT NOT NULL DEFAULT '';
ALTER TABLE output_command ADD COLUMN external_id TEXT NOT NULL DEFAULT '';

-- Hive sessions this app created on an inbox item's behalf. Keyed by hive's
-- session id, which is stable across a rename; the slug is not, and the name
-- is not. Everything else about the session — its name, state, checkout and
-- liveness — is read live from hive rather than mirrored here, so this table
-- can never disagree with the session it points at.
--
-- The item is identified by its durable coordinates rather than inbox_item.id
-- (and so carries no foreign key): those four columns are inbox_item's own
-- UNIQUE key, so a row pruned by retention and re-ingested later resolves to
-- the same sessions, and a session outliving its item is not a dangling
-- reference. A link whose session hive no longer has is pruned when it is read.
CREATE TABLE item_session (
    session_id   TEXT PRIMARY KEY,
    profile_id   TEXT NOT NULL,
    source_kind  TEXT NOT NULL,
    source_scope TEXT NOT NULL,
    external_id  TEXT NOT NULL,
    created_at   INTEGER NOT NULL
) STRICT;

CREATE INDEX idx_item_session_item
    ON item_session(profile_id, source_kind, source_scope, external_id, created_at DESC);
