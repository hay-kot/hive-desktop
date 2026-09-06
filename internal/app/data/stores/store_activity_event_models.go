package stores

// ActivityEvent is one durable row in the Activity view's audit log.
// Category and severity stay plain strings here because this leaf package
// does not import the activity package's typed enums. Metadata round-trips
// as a map; the store owns the JSON encoding underneath it.
type ActivityEvent struct {
	ID        int64             `json:"id"`
	CreatedAt int64             `json:"createdAt"`
	Category  string            `json:"category"`
	Severity  string            `json:"severity"`
	Title     string            `json:"title"`
	Body      string            `json:"body"`
	Source    string            `json:"source"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// ActivityEventCreate is Append's input: CreatedAt is assigned by the store.
type ActivityEventCreate struct {
	Category string
	Severity string
	Title    string
	Body     string
	Source   string
	Metadata map[string]string
}
