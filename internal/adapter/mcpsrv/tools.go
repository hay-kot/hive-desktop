package mcpsrv

import "github.com/modelcontextprotocol/go-sdk/mcp"

// noInput is the input type of a tool that takes no arguments. The SDK
// requires a struct or map so the inferred schema has type "object".
type noInput struct{}

// register declares every tool this adapter serves.
//
// This is the tool table: the one place a tool is declared, carrying metadata
// and nothing else. Dispatch is the handler's, and every handler is a thin
// call into *app.App — the same rule httpapi's operations table follows, and
// the reason a registry used for enumeration never carries dispatch.
//
// Input schemas are inferred from each handler's typed input struct, so a tool
// cannot advertise a field its handler does not accept. Descriptions are
// written for a model rather than for a person reading a reference: what the
// tool answers, what it will not do, and what a surprising answer means.
func (ctrl *Controller) register(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:  "get_status",
		Title: "Desktop status",
		Description: "Report the running build and whether the webhook listener is up, on which host and port. " +
			"Use this first to confirm the app is reachable and to discover the webhook base URL a push should go to.",
	}, ctrl.GetStatus)

	mcp.AddTool(srv, &mcp.Tool{
		Name:  "list_profiles",
		Title: "List profiles",
		Description: "List every profile with its load status and whether it has an avatar. " +
			"Profile ids from here are what every other tool's profile argument takes. " +
			"A profile whose flow file does not parse still lists, with valid=false.",
	}, ctrl.ListProfiles)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_feeds",
		Title:       "List feeds",
		Description: "List a profile's feeds with their unread and archived counts.",
	}, ctrl.ListFeeds)

	mcp.AddTool(srv, &mcp.Tool{
		Name:  "list_inbox",
		Title: "List inbox items",
		Description: "List a profile's inbox items, optionally scoped to one feed or resolved by the source's own external id. " +
			"Each item carries the id of the feed claiming it (empty when unrouted) and the source's raw JSON payload.",
	}, ctrl.ListInbox)

	mcp.AddTool(srv, &mcp.Tool{
		Name:  "list_inbox_item_events",
		Title: "List an inbox item's events",
		Description: "List one inbox item's lifecycle events, resolved by itemId or by an externalId that matches exactly one item. " +
			"Each event's detail is source-specific raw JSON. An externalId matching items in more than one profile is a conflict — pass profile to disambiguate.",
	}, ctrl.ListInboxItemEvents)

	mcp.AddTool(srv, &mcp.Tool{
		Name:  "list_actions",
		Title: "List the action catalog",
		Description: "List the action catalog with the actions.yml it was loaded from and whether that file currently parses. " +
			"An invalid edit leaves the previous catalog in effect and reports its error here, so this is how to confirm an edit actually loaded.",
	}, ctrl.ListActions)

	mcp.AddTool(srv, &mcp.Tool{
		Name:  "refresh_sources",
		Title: "Force a source refresh",
		Description: "Force one producer tick across all sources, dropping fetch caches. " +
			"Returns aggregate totals rather than a per-source breakdown. Use it after changing a flow or pushing a test event instead of waiting for the next poll.",
	}, ctrl.RefreshSources)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_profile",
		Title:       "Create a profile",
		Description: "Create a profile, seeded with the starter graph when exactly one GitHub account is connected and empty otherwise.",
	}, ctrl.CreateProfile)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "delete_profile",
		Title:       "Delete a profile",
		Description: "Delete a profile, its flow files, its avatar, and its inbox state. This is not reversible.",
	}, ctrl.DeleteProfile)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_profile_image",
		Title:       "Read a profile's avatar",
		Description: "Return a profile's avatar as a PNG image. A profile with no avatar is not_found — its rail falls back to a letter chip.",
	}, ctrl.GetProfileImage)

	mcp.AddTool(srv, &mcp.Tool{
		Name:  "set_profile_image",
		Title: "Set a profile's avatar",
		Description: "Set a profile's avatar from base64-encoded image bytes (PNG, JPEG, GIF or WebP — the format is sniffed, so no media type is needed). " +
			"The image is normalized to a 128x128 PNG.",
	}, ctrl.SetProfileImage)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "clear_profile_image",
		Title:       "Clear a profile's avatar",
		Description: "Clear a profile's avatar so its rail reverts to the letter chip.",
	}, ctrl.ClearProfileImage)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_node_image",
		Title:       "Read a webhook source's feed mark",
		Description: "Return a webhook source node's feed-mark image as a PNG. A node with no mark is not_found — its items fall back to the node's glyph.",
	}, ctrl.GetNodeImage)

	mcp.AddTool(srv, &mcp.Tool{
		Name:  "set_node_image",
		Title: "Set a webhook source's feed mark",
		Description: "Set a webhook source node's feed-mark image from base64-encoded image bytes (PNG, JPEG, GIF or WebP). " +
			"It is normalized to a 128x128 PNG and shown on that source's items instead of its icon.",
	}, ctrl.SetNodeImage)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "clear_node_image",
		Title:       "Clear a webhook source's feed mark",
		Description: "Clear a webhook source node's feed-mark image so its items revert to the node's icon.",
	}, ctrl.ClearNodeImage)

	mcp.AddTool(srv, &mcp.Tool{
		Name:  "execute_flow",
		Title: "Dry-run a flow",
		Description: "Dry-run a flow against input you supply and report what every node did, committing nothing — no feed membership, " +
			"inbox rows, notifications, queued actions or durable kv. Name the flow with exactly one of flowId (an installed flow, enabled or not), " +
			"flow (a flow document as a JSON object, same schema as flows/<id>.yaml, version included) or flowYaml (that document as YAML text), " +
			"so an unsaved edit can be executed before it is deployed. Messages are delivered to nodeId — any node, not only a source, which is how " +
			"one function node is exercised in isolation against a captured payload. Sources never fetch: a source node relays what you inject. " +
			"Envelope fields left empty are filled in. kv seeds an in-memory sandbox (nodeId -> key -> value) that is the whole world a kv.get sees, " +
			"so notify-once logic is testable against a known starting state; what the run would have written comes back in kvMutations. " +
			"Each node reports what it received, what it emitted per output port, its drops, timing, console output, and a structured error with " +
			"line and column for a script failure. Prefer this over deploying an edit and waiting for a poll.",
	}, ctrl.ExecuteFlow)
}
