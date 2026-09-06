package stores

// FeedClaim is one item's membership in one feed, attributed to the source
// that produced it. It has no store-level mapper: every FeedClaimStore
// method either writes one or deletes by criteria, so a row of this shape
// only ever arrives as a caller-built value (the engine's replay
// recomputation), never as a read result.
type FeedClaim struct {
	ProfileID string `json:"profileId"`
	FeedID    string `json:"feedId"`
	ItemID    int64  `json:"itemId"`
	SourceID  string `json:"sourceId"`
}
