-- name: AppendEvent :one
INSERT INTO event_log (topic, key, payload, created_at, snapshot, source_kind, source_scope, occurrence_key)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING "offset";

-- name: UpdateEventOccurrenceKey :exec
UPDATE event_log SET occurrence_key = ? WHERE "offset" = ?;

-- name: GetSourceHeadPayload :one
SELECT payload FROM source_head
WHERE topic = ? AND key = ?;

-- name: UpsertSourceHead :exec
INSERT INTO source_head (topic, key, payload)
VALUES (?, ?, ?)
ON CONFLICT(topic, key) DO UPDATE SET payload = excluded.payload;

-- name: DeleteSourceHead :exec
DELETE FROM source_head WHERE topic = ? AND key = ?;

-- name: ListActiveSourceHeadKeys :many
SELECT h.key
FROM source_head h
JOIN inbox_item i
  ON i.external_id = h.key
 AND i.profile_id = sqlc.arg(profile_id)
 AND i.source_kind = sqlc.arg(source_kind)
 AND i.source_scope = sqlc.arg(source_scope)
WHERE h.topic = sqlc.arg(topic)
  AND i.archived_at IS NULL;

-- name: ReadEventsFrom :many
SELECT * FROM event_log
WHERE "offset" > ?
ORDER BY "offset" ASC
LIMIT ?;

-- name: GetConsumerOffset :one
SELECT * FROM consumer_offset
WHERE consumer = ?;

-- name: DeleteEventsOlderThan :exec
DELETE FROM event_log AS target
WHERE target.created_at < ?
  AND NOT (
      target.snapshot = 1
      AND target."offset" = (
          SELECT MAX(snapshot_row."offset")
          FROM event_log AS snapshot_row
          WHERE snapshot_row.topic = target.topic AND snapshot_row.snapshot = 1
      )
  );

-- name: DeleteEventsOverLimitPerTopic :exec
DELETE FROM event_log AS target
WHERE (
    SELECT COUNT(*) FROM event_log AS newer
    WHERE newer.topic = target.topic AND newer."offset" > target."offset"
) >= CAST(sqlc.arg(limit) AS INTEGER)
  AND NOT (
      target.snapshot = 1
      AND target."offset" = (
          SELECT MAX(snapshot_row."offset")
          FROM event_log AS snapshot_row
          WHERE snapshot_row.topic = target.topic AND snapshot_row.snapshot = 1
      )
  );

-- name: DeleteSnapshotsOverLimitPerTopic :exec
-- Superseded source snapshots are dead weight: replay and every snapshot read
-- take only the newest snapshot per topic, so older ones reconcile nothing.
-- Keep the newest N snapshots per topic and delete the rest. This is the bound
-- that actually caps event_log size, because a full-source snapshot dwarfs an
-- ordinary item event; the per-topic row bound above counts item events too
-- and so cannot target the snapshots that dominate. N is >= 1, so the single
-- latest snapshot the age/count bounds preserve is preserved here as well.
DELETE FROM event_log AS target
WHERE target.snapshot = 1
  AND (
      SELECT COUNT(*) FROM event_log AS newer
      WHERE newer.topic = target.topic
        AND newer.snapshot = 1
        AND newer."offset" > target."offset"
  ) >= CAST(sqlc.arg(limit) AS INTEGER);

-- name: CommitConsumerOffset :exec
-- Monotonic upsert: on conflict, only advance the stored offset when the
-- incoming one is greater. If it isn't, the WHERE clause makes the DO UPDATE
-- a no-op (SQLite upsert semantics), so a stale/out-of-order commit never
-- regresses a consumer's checkpoint.
INSERT INTO consumer_offset (consumer, "offset")
VALUES (?, ?)
ON CONFLICT(consumer) DO UPDATE SET "offset" = excluded."offset"
WHERE excluded."offset" > consumer_offset."offset";

-- name: InsertInboxItem :one
-- Seed-only plain insert for deterministic desktop fixtures. The ingestion
-- path receives its classify-and-upsert contract in a later phase.
INSERT INTO inbox_item (
    profile_id, source_kind, source_scope, external_id,
    title, url, payload, revision, unread,
    lifecycle, first_seen_at, last_event_at
) VALUES (
    ?, ?, ?, ?,
    ?, ?, ?, 1, ?,
    ?, ?, ?
)
RETURNING *;

-- name: UpsertInboxItem :one
INSERT INTO inbox_item (
    profile_id, source_kind, source_scope, external_id,
    title, url, payload, revision, unread,
    archived_at, archived_actor, archived_reason,
    lifecycle, source_state, first_seen_at, last_event_at
) VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (profile_id, source_kind, source_scope, external_id) DO UPDATE SET
    title = excluded.title, url = excluded.url, payload = excluded.payload,
    revision = inbox_item.revision + 1, unread = excluded.unread,
    archived_at = excluded.archived_at, archived_actor = excluded.archived_actor,
    archived_reason = excluded.archived_reason, lifecycle = excluded.lifecycle,
    source_state = excluded.source_state, last_event_at = excluded.last_event_at
RETURNING *;

-- name: GetInboxItemByExternalID :one
SELECT * FROM inbox_item
WHERE profile_id = ? AND source_kind = ? AND source_scope = ? AND external_id = ?;

-- name: RescopeInboxItem :exec
-- Move a row to a new source_scope. Used to heal pre-#63 rows written with an
-- empty scope onto the account-scoped identity every post-#63 read keys on
-- (issue #95); the caller only rescopes when the target identity is free, so
-- this never collides with the UNIQUE (profile_id, source_kind, source_scope,
-- external_id) index.
UPDATE inbox_item SET source_scope = sqlc.arg(source_scope) WHERE id = sqlc.arg(id);

-- name: FindInboxItemsByExternalID :many
SELECT * FROM inbox_item
WHERE external_id = sqlc.arg(external_id)
  AND (sqlc.arg(profile_id) = '' OR profile_id = sqlc.arg(profile_id))
ORDER BY profile_id, source_kind, source_scope;

-- name: ListAllInboxItems :many
SELECT * FROM inbox_item
WHERE (sqlc.arg(profile_id) = '' OR profile_id = sqlc.arg(profile_id))
ORDER BY last_event_at DESC, id DESC
LIMIT sqlc.arg(lim);

-- name: ListUnarchivedInboxItemsByProfile :many
SELECT * FROM inbox_item WHERE profile_id = ? AND archived_at IS NULL;

-- name: ListLatestSourceSnapshotsByTopicPrefix :many
SELECT event_log.*
FROM event_log
JOIN (
    SELECT topic, MAX("offset") AS "offset"
    FROM event_log
    WHERE snapshot = 1
      AND "offset" <= CAST(sqlc.arg(through_offset) AS INTEGER)
      AND substr(topic, 1, length(CAST(sqlc.arg(topic_prefix) AS TEXT))) = CAST(sqlc.arg(topic_prefix) AS TEXT)
    GROUP BY topic
) AS latest ON latest.topic = event_log.topic AND latest."offset" = event_log."offset"
ORDER BY event_log.topic;

-- name: GetUnarchivedInboxItemByID :one
SELECT * FROM inbox_item WHERE id = ? AND profile_id = ? AND archived_at IS NULL;

-- name: GetInboxItemByID :one
SELECT * FROM inbox_item WHERE id = ?;

-- Every user-facing feed view is claim-scoped: source observations that did
-- not reach a feed terminal remain internal pipeline data and never leak into
-- workspace categories. Trash is the only exception: it deliberately surfaces
-- unrouted and ignored items so users can debug rules and un-ignore.
-- name: ListInboxItemsByFeed :many
SELECT DISTINCT i.* FROM inbox_item i
JOIN feed_membership_claim c ON c.item_id = i.id
WHERE c.profile_id = ? AND c.feed_id = ? AND i.archived_at IS NULL AND i.ignored_at IS NULL
ORDER BY i.last_event_at DESC, i.id DESC LIMIT ?;

-- Archived items stay visible inside the feeds they matched, demoted below
-- active rows and lazy-loaded when the archived section expands.
-- name: ListArchivedInboxItemsByFeed :many
SELECT DISTINCT i.* FROM inbox_item i
JOIN feed_membership_claim c ON c.item_id = i.id
WHERE c.profile_id = ? AND c.feed_id = ? AND i.archived_at IS NOT NULL AND i.ignored_at IS NULL
ORDER BY i.archived_at DESC, i.id DESC LIMIT ?;

-- Trash holds items with no path to any feed (routing produced no claims)
-- plus user-ignored items. It is a utility/debug surface, never a queue.
-- name: ListInboxItemsTrash :many
SELECT i.* FROM inbox_item i
WHERE i.profile_id = ?
  AND (i.ignored_at IS NOT NULL
       OR NOT EXISTS (SELECT 1 FROM feed_membership_claim c WHERE c.profile_id = i.profile_id AND c.item_id = i.id))
ORDER BY i.last_event_at DESC, i.id DESC LIMIT ?;

-- name: CountInboxItemsByFeed :many
SELECT
    c.feed_id AS feed_id,
    CAST(COUNT(DISTINCT CASE WHEN i.archived_at IS NULL THEN i.id END) AS INTEGER) AS total,
    CAST(COUNT(DISTINCT CASE WHEN i.archived_at IS NULL AND i.unread = 1 THEN i.id END) AS INTEGER) AS unread,
    CAST(COUNT(DISTINCT CASE WHEN i.archived_at IS NOT NULL THEN i.id END) AS INTEGER) AS archived
FROM feed_membership_claim c
JOIN inbox_item i ON i.id = c.item_id
WHERE c.profile_id = ? AND i.ignored_at IS NULL
GROUP BY c.feed_id;

-- name: ListInboxEventsByItem :many
SELECT * FROM inbox_event WHERE item_id = ? ORDER BY id DESC LIMIT ?;

-- name: SetInboxItemUnread :one
UPDATE inbox_item SET unread = ?, revision = revision + 1
WHERE id = ? AND revision = ?
RETURNING *;

-- Bulk read-state clear, claim-scoped so it covers exactly the rows
-- CountInboxItemsByFeed reports as unread. Deliberately NOT revision-guarded:
-- a bulk triage has no single row whose revision the caller could have read,
-- and read state is low-stakes. The revision still advances, so an optimistic
-- per-item write holding a pre-clear copy is rejected and re-reads instead of
-- silently resurrecting the unread flag. Archived and ignored rows are left
-- alone: neither is in the active list this clears.
-- (Keep this file ASCII-only. sqlc mixes byte and rune offsets when it edits a
-- query, so a multi-byte character anywhere above corrupts a later query.)
-- name: MarkFeedInboxItemsRead :execrows
UPDATE inbox_item SET unread = 0, revision = revision + 1
WHERE unread = 1 AND archived_at IS NULL AND ignored_at IS NULL
  AND id IN (SELECT c.item_id FROM feed_membership_claim c WHERE c.profile_id = ? AND c.feed_id = ?);

-- The workspace-wide variant: every feed in the profile, same scoping rules.
-- Unrouted observations stay untouched; they live only in Trash, which carries
-- no unread semantics.
-- name: MarkProfileInboxItemsRead :execrows
UPDATE inbox_item SET unread = 0, revision = revision + 1
WHERE unread = 1 AND archived_at IS NULL AND ignored_at IS NULL
  AND id IN (SELECT c.item_id FROM feed_membership_claim c WHERE c.profile_id = ?);

-- name: ToggleInboxItemArchived :one
UPDATE inbox_item SET
    ignored_at = CASE WHEN archived_at IS NULL THEN NULL ELSE ignored_at END,
    archived_at = CASE WHEN archived_at IS NULL THEN ? ELSE NULL END,
    archived_actor = CASE WHEN archived_at IS NULL THEN 'manual' ELSE NULL END,
    archived_reason = CASE WHEN archived_at IS NULL THEN 'manual' ELSE NULL END,
    revision = revision + 1
WHERE id = ? AND revision = ?
RETURNING *;

-- name: ToggleInboxItemIgnored :one
UPDATE inbox_item SET
    ignored_at = CASE WHEN ignored_at IS NULL THEN ? ELSE NULL END,
    unread = CASE WHEN ignored_at IS NULL THEN 0 ELSE 1 END,
    archived_at = CASE WHEN ignored_at IS NULL THEN NULL ELSE archived_at END,
    archived_actor = CASE WHEN ignored_at IS NULL THEN NULL ELSE archived_actor END,
    archived_reason = CASE WHEN ignored_at IS NULL THEN NULL ELSE archived_reason END,
    revision = revision + 1
WHERE id = ? AND revision = ?
RETURNING *;

-- name: UpsertFeedMembershipClaim :exec
INSERT INTO feed_membership_claim (profile_id, feed_id, item_id, source_id)
VALUES (?, ?, ?, ?)
ON CONFLICT (profile_id, feed_id, item_id, source_id) DO NOTHING;

-- name: DeleteFeedMembershipClaimsNotInSnapshot :exec
DELETE FROM feed_membership_claim
WHERE feed_id = ? AND source_id = ? AND item_id NOT IN (sqlc.slice(item_ids));

-- name: DeleteFeedMembershipClaimsForSourceAll :exec
DELETE FROM feed_membership_claim WHERE feed_id = ? AND source_id = ?;

-- name: DeleteFeedMembershipClaimsForFeeds :exec
DELETE FROM feed_membership_claim
WHERE feed_membership_claim.profile_id = ? AND feed_id NOT IN (sqlc.slice(feed_ids));

-- name: DeleteFeedMembershipClaimsForFeedsAll :exec
DELETE FROM feed_membership_claim WHERE profile_id = ?;

-- name: DeleteFeedMembershipClaimsForRemovedSources :exec
DELETE FROM feed_membership_claim
WHERE feed_membership_claim.profile_id = ?
  AND source_id NOT IN (sqlc.slice(source_ids))
  AND item_id IN (SELECT id FROM inbox_item WHERE archived_at IS NULL);

-- name: DeleteFeedMembershipClaimsForRemovedSourcesAll :exec
DELETE FROM feed_membership_claim
WHERE feed_membership_claim.profile_id = ?
  AND item_id IN (SELECT id FROM inbox_item WHERE archived_at IS NULL);

-- name: DeleteUnarchivedFeedMembershipClaimsByProfile :exec
DELETE FROM feed_membership_claim
WHERE feed_membership_claim.profile_id = ? AND item_id IN (SELECT id FROM inbox_item WHERE archived_at IS NULL);

-- name: DeleteInboxItemsByProfile :exec
DELETE FROM inbox_item WHERE profile_id = ?;

-- name: DeleteConsumerOffsetByConsumer :exec
DELETE FROM consumer_offset WHERE consumer = ?;

-- name: DeleteEventLogByTopicPrefix :exec
DELETE FROM event_log WHERE topic LIKE ? ESCAPE '\';

-- name: DeleteSourceHeadByTopicPrefix :exec
DELETE FROM source_head WHERE topic LIKE ? ESCAPE '\';

-- name: PruneArchivedInboxItems :exec
-- Cascades to inbox_event and feed_membership_claim through their item FKs.
DELETE FROM inbox_item WHERE archived_at IS NOT NULL AND archived_at <= ?;

-- name: DeleteOrphanedSourceHeads :exec
DELETE FROM source_head
WHERE key NOT IN (SELECT external_id FROM inbox_item);

-- name: InsertInboxEvent :one
INSERT INTO inbox_event (item_id, kind, transition, attention, occurrence_key, summary, detail, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (item_id, occurrence_key) WHERE occurrence_key IS NOT NULL DO NOTHING
RETURNING *;

-- name: EnqueueOutputCommand :exec
-- Deduped on (action_id, key): a replayed commit batch enqueues the same
-- action invocation at most once (see idx_output_command_action_key).
INSERT INTO output_command (action_id, key, payload, status, created_at)
VALUES (?, ?, ?, 'pending', ?)
ON CONFLICT DO NOTHING;

-- name: ListRunnableOutputCommands :many
-- Every enqueued flow action is runnable immediately; running is reserved for
-- an explicit detail invocation.
SELECT * FROM output_command
WHERE status = 'pending'
ORDER BY id ASC
LIMIT ?;

-- name: ListRunnableOutputCommandsAfter :many
-- Continue a bounded worker scan after the previous row. The status/id
-- predicate is covered by idx_output_command_status_id.
SELECT * FROM output_command
WHERE status = 'pending' AND id > ?
ORDER BY id ASC
LIMIT ?;

-- name: ConfirmOutputCommand :one
-- Explicit detail invocation creates work or claims a queued flow command.
-- Terminal/running commands remain deduplicated.
INSERT INTO output_command (action_id, key, payload, status, created_at)
VALUES (?, ?, ?, 'running', ?)
ON CONFLICT DO UPDATE SET status = 'running'
WHERE output_command.status = 'pending'
RETURNING *;

-- name: RerunOutputCommand :one
-- An explicit user confirmation creates a separate command so prior execution
-- diagnostics and Activity links remain intact.
INSERT INTO output_command (action_id, key, payload, status, created_at, is_rerun)
SELECT sqlc.arg(action_id), sqlc.arg(key), sqlc.arg(payload), 'running', sqlc.arg(created_at), 1
WHERE EXISTS (
    SELECT 1 FROM output_command
    WHERE action_id = sqlc.arg(action_id) AND key = sqlc.arg(key)
      AND status IN ('done', 'failed')
)
AND NOT EXISTS (
    SELECT 1 FROM output_command
    WHERE action_id = sqlc.arg(action_id) AND key = sqlc.arg(key)
      AND status IN ('pending', 'running')
)
RETURNING *;

-- name: GetLatestOutputCommandForAction :one
SELECT * FROM output_command
WHERE action_id = ? AND key = ?
ORDER BY id DESC
LIMIT 1;

-- name: GetOutputCommand :one
SELECT * FROM output_command WHERE id = ?;

-- name: MarkOutputCommandDone :exec
UPDATE output_command
SET status = 'done', last_error = NULL, result_json = ?, stdout = ?, stderr = ?
WHERE id = ?;

-- name: RetryOutputCommand :exec
UPDATE output_command SET attempts = attempts + 1, last_error = ?, stdout = ?, stderr = ?
WHERE id = ?;

-- name: MarkOutputCommandFailed :exec
UPDATE output_command SET status = 'failed', attempts = attempts + 1, last_error = ?, stdout = ?, stderr = ?
WHERE id = ?;

-- name: InsertNodeRun :exec
INSERT INTO node_run (flow_id, node_id, ok, in_count, out_count, drop_count, err, ended_at, dur_ms)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListNodeRunsByFlow :many
-- Recent runs, newest first: the canvas derives latest-per-node status and a
-- RECENT list from this page rather than querying per-node.
SELECT * FROM node_run
WHERE flow_id = ?
ORDER BY ended_at DESC
LIMIT ?;

-- name: PruneNodeRuns :exec
-- Retain the newest rows globally. rowid breaks same-nanosecond ties, so the
-- limit is exact even when a fast batch stamps equal ended_at values.
DELETE FROM node_run
WHERE rowid IN (
    SELECT rowid FROM node_run
    ORDER BY ended_at DESC, rowid DESC
    LIMIT -1 OFFSET ?
);

-- name: PruneTerminalOutputCommands :exec
-- Never remove active commands: only terminal done/failed history is bounded.
DELETE FROM output_command
WHERE id IN (
    SELECT id FROM output_command
    WHERE status IN ('done', 'failed')
    ORDER BY id DESC
    LIMIT -1 OFFSET ?
);

-- name: AppendActivityEvent :one
-- Append one audit-log row and return it (with its assigned id) so the caller
-- can echo the stored event straight back to subscribers.
INSERT INTO activity_event (created_at, category, severity, title, body, source, metadata)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListActivityEvents :many
-- Newest first, paged by a descending id cursor: pass a sentinel above the
-- largest id for the first page, then the smallest id returned to continue.
SELECT * FROM activity_event
WHERE id < ?
ORDER BY id DESC
LIMIT ?;

-- name: PruneActivityEvents :exec
-- Retain only the newest rows globally; bounded diagnostic history like
-- node_run. The id primary key both orders and breaks ties exactly.
DELETE FROM activity_event
WHERE id IN (
    SELECT id FROM activity_event
    ORDER BY id DESC
    LIMIT -1 OFFSET ?
);

-- name: InsertJob :one
INSERT INTO job (created_at, updated_at, status, label, step, action_id, target, error, command_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: SetJobRunning :one
-- Advance a job to running and link its output_command. This is the ONLY write
-- that sets command_id, so terminal transitions can never null it out.
UPDATE job SET updated_at = ?, status = ?, step = ?, command_id = ?
WHERE id = ?
RETURNING *;

-- name: SetJobStatus :one
-- Advance a job's status/step/error WITHOUT touching command_id (used by the
-- done/failed terminal transitions). Splitting this from SetJobRunning avoids a
-- read-modify-write and prevents clobbering the command link.
UPDATE job SET updated_at = ?, status = ?, step = ?, error = ?
WHERE id = ?
RETURNING *;

-- name: FindRunningJobByCommandID :one
-- Restore a running job's identity after a worker or app restart so retries do
-- not create duplicate jobs and strand the original lifecycle as active.
SELECT * FROM job
WHERE command_id = ? AND status = 'running'
ORDER BY id DESC
LIMIT 1;

-- name: ListJobs :many
-- Newest first, paged by a descending id cursor (same shape as ListActivityEvents).
SELECT * FROM job WHERE id < ? ORDER BY id DESC LIMIT ?;

-- name: ListActiveJobs :many
-- Non-terminal jobs plus terminal jobs updated within a recency window, newest
-- first. Drives the auto-hiding titlebar chip.
SELECT * FROM job
WHERE status IN ('queued', 'running') OR (status IN ('done', 'failed') AND updated_at >= ?)
ORDER BY id DESC;

-- name: PruneTerminalJobs :exec
-- Never remove active jobs: only terminal done/failed history is bounded
-- (mirrors PruneTerminalOutputCommands).
DELETE FROM job
WHERE id IN (
    SELECT id FROM job
    WHERE status IN ('done', 'failed')
    ORDER BY id DESC
    LIMIT -1 OFFSET ?
);

-- name: UpsertWebhookCapture :exec
INSERT INTO webhook_capture (topic, received_at, body)
VALUES (?, ?, ?)
ON CONFLICT (topic) DO UPDATE SET received_at = excluded.received_at, body = excluded.body;

-- name: GetWebhookCapture :one
SELECT * FROM webhook_capture WHERE topic = ?;

-- name: ListUnarchivedInboxItemsBySource :many
-- The complete current item set of one source identity, oldest activity
-- first: the webhook listener builds its per-delivery authoritative
-- snapshot from these rows.
SELECT * FROM inbox_item
WHERE profile_id = ? AND source_kind = ? AND source_scope = ? AND archived_at IS NULL
ORDER BY last_event_at ASC, id ASC;

-- name: CountNonterminalCommandsForAction :one
-- Pending work can still run and running work may already have been
-- dispatched, so either blocks deleting the action. Done and failed history
-- is terminal and must never keep an action from deletion.
SELECT COUNT(*) FROM output_command
WHERE action_id = ? AND status IN ('pending', 'running');

-- name: GetNodeKV :one
-- Expiry-aware read: an expired row reads as absent (sql.ErrNoRows). The
-- final arg is an explicit `now` cutoff -- never a fresh time.Now() inside
-- the query -- so the expiry boundary is deterministically testable.
SELECT value FROM node_kv
WHERE flow_id = ? AND node_id = ? AND scope = ? AND key = ?
  AND (expires_at IS NULL OR expires_at > ?);

-- name: UpsertNodeKV :exec
INSERT INTO node_kv (flow_id, node_id, scope, key, value, expires_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (flow_id, node_id, scope, key) DO UPDATE SET
    value = excluded.value, expires_at = excluded.expires_at, updated_at = excluded.updated_at;

-- name: DeleteNodeKV :exec
DELETE FROM node_kv WHERE flow_id = ? AND node_id = ? AND scope = ? AND key = ?;

-- name: ListNodeKVKeysByPrefix :many
-- Unexpired keys under one node/scope whose key has the given prefix. Uses
-- GLOB, which is binary/case-SENSITIVE, so it matches Go strings.HasPrefix
-- exactly; default LIKE is ASCII case-insensitive and would silently
-- disagree on a mixed-case prefix. The wrapper escapes GLOB metacharacters
-- in the prefix and appends `*`; a literal leading prefix lets SQLite
-- range-scan the composite PK index. The final arg is an explicit `now`
-- cutoff.
SELECT key FROM node_kv
WHERE flow_id = ? AND node_id = ? AND scope = ?
  AND key GLOB sqlc.arg(pattern)
  AND (expires_at IS NULL OR expires_at > sqlc.arg(now))
ORDER BY key;

-- name: DeleteExpiredNodeKV :exec
DELETE FROM node_kv WHERE expires_at IS NOT NULL AND expires_at <= ?;

-- name: DeleteNodeKVByFlow :exec
DELETE FROM node_kv WHERE flow_id = ?;

-- name: DeleteNodeKVForFlowExceptNodes :exec
-- The empty-slice case is invalid SQL (NOT IN ()); callers with no node ids
-- to retain use DeleteNodeKVByFlow instead, mirroring the
-- DeleteFeedMembershipClaimsForFeeds / *All pair.
DELETE FROM node_kv WHERE flow_id = ? AND node_id NOT IN (sqlc.slice(node_ids));

-- name: ListAgentWorkspaceSessions :many
-- One workspace's sessions, most recently opened first: the order the area
-- lists them in.
SELECT * FROM agent_workspace_session
WHERE workspace = ?
ORDER BY last_opened_at DESC;

-- name: GetAgentWorkspaceSession :one
SELECT * FROM agent_workspace_session WHERE id = ?;

-- name: InsertAgentWorkspaceSession :one
INSERT INTO agent_workspace_session (workspace, name, agent, agent_session_id, created_at, last_opened_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: TouchAgentWorkspaceSession :exec
UPDATE agent_workspace_session SET last_opened_at = ? WHERE id = ?;

-- name: SetAgentWorkspaceSessionAgentID :exec
-- The only write to agent_session_id after the insert: a resume that falls
-- back to a fresh launch (the agent has no resume form) mints a new id and
-- records it here so a later resume of this same record addresses the
-- conversation actually running rather than the one it replaced.
UPDATE agent_workspace_session SET agent_session_id = ? WHERE id = ?;

-- name: DeleteAgentWorkspaceSession :exec
DELETE FROM agent_workspace_session WHERE id = ?;

-- name: DeleteAgentWorkspaceSessionsByWorkspace :exec
DELETE FROM agent_workspace_session WHERE workspace = ?;
