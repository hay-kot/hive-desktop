package stores

import "github.com/hay-kot/hive-desktop/internal/app/data/queries"

// ActivityEvent is one durable row in the Activity view's audit log.
// Category and severity stay plain strings here because this leaf package
// does not import the activity package's typed enums. Metadata is raw JSON,
// nil when the row carries none.
type ActivityEvent struct {
	ID        int64  `json:"id"`
	CreatedAt int64  `json:"createdAt"`
	Category  string `json:"category"`
	Severity  string `json:"severity"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Source    string `json:"source"`
	Metadata  []byte `json:"metadata,omitempty"`
}

// ActivityEventCreate is Append's input: CreatedAt is assigned by the store.
type ActivityEventCreate struct {
	Category string
	Severity string
	Title    string
	Body     string
	Source   string
	Metadata []byte
}

func mapActivityEventFromDB(row queries.ActivityEvent) ActivityEvent {
	return ActivityEvent{
		ID: row.ID, CreatedAt: row.CreatedAt, Category: row.Category, Severity: row.Severity,
		Title: row.Title, Body: row.Body, Source: row.Source, Metadata: row.Metadata,
	}
}
