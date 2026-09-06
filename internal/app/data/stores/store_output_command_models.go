package stores

import (
	"github.com/hay-kot/hive-desktop/internal/app/data/models"
	"github.com/hay-kot/hive-desktop/internal/app/data/queries"
)

// OutputCommand is one enqueued or completed action invocation.
type OutputCommand struct {
	ID          int64
	ActionID    string
	Key         string
	Payload     []byte
	Status      string
	Attempts    int64
	LastError   string
	ResultJSON  string
	Stdout      string
	Stderr      string
	CreatedAt   int64
	IsRerun     bool
	ProfileID   string
	SourceKind  string
	SourceScope string
	ExternalID  string
}

// ItemRef is the inbox item this command was routed from. A command with no
// inbox origin -- a notify command, or an action invoked from a surface that
// has no item behind it -- returns a zero ref, which reads as not Known.
func (c OutputCommand) ItemRef() models.ItemRef {
	return models.ItemRef{ProfileID: c.ProfileID, SourceKind: c.SourceKind, SourceScope: c.SourceScope, ExternalID: c.ExternalID}
}

func mapOutputCommandFromDB(row queries.OutputCommand) OutputCommand {
	return OutputCommand{
		ID: row.ID, ActionID: row.ActionID, Key: row.Key, Payload: row.Payload,
		Status: row.Status, Attempts: row.Attempts, LastError: row.LastError.String,
		ResultJSON: row.ResultJson.String, Stdout: row.Stdout.String, Stderr: row.Stderr.String,
		CreatedAt: row.CreatedAt, IsRerun: row.IsRerun != 0,
		ProfileID: row.ProfileID, SourceKind: row.SourceKind, SourceScope: row.SourceScope, ExternalID: row.ExternalID,
	}
}
