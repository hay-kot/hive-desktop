package stores

// SourceIdentity scopes an active source head listing to the inbox rows one
// connector instance owns.
type SourceIdentity struct {
	Topic       string
	ProfileID   string
	SourceKind  string
	SourceScope string
}
