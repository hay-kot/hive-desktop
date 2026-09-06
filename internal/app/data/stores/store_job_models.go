package stores

import "github.com/hay-kot/hive-desktop/internal/app/data/queries"

// Job is the storage shape of one job row. Status and step stay plain
// strings at this layer; the jobs package owns the typed status and converts
// at its boundary. CommandID is nil until the job is linked to an
// output_command.
type Job struct {
	ID        int64  `json:"id"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
	Status    string `json:"status"`
	Label     string `json:"label"`
	Step      string `json:"step"`
	ActionID  string `json:"actionId"`
	Target    string `json:"target"`
	Error     string `json:"error"`
	CommandID *int64 `json:"commandId,omitempty"`
}

// JobCreate is Insert's input: CreatedAt and UpdatedAt are assigned by the
// store.
type JobCreate struct {
	Status   string
	Label    string
	Step     string
	ActionID string
	Target   string
	Error    string
}

func mapJobFromDB(row queries.Job) Job {
	var commandID *int64
	if row.CommandID.Valid {
		value := row.CommandID.Int64
		commandID = &value
	}
	return Job{
		ID: row.ID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, Status: row.Status,
		Label: row.Label, Step: row.Step, ActionID: row.ActionID, Target: row.Target,
		Error: row.Error, CommandID: commandID,
	}
}
