package stores

import "github.com/hay-kot/hive-desktop/internal/app/data/queries"

// AgentSession is one durable record of an agent workspace session: the
// launch table entry a terminal is resolved from and reattached through.
type AgentSession struct {
	ID             int64  `json:"id"`
	Workspace      string `json:"workspace"`
	Name           string `json:"name"`
	Agent          string `json:"agent"`
	AgentSessionID string `json:"agentSessionId"`
	CreatedAt      int64  `json:"createdAt"`
	LastOpenedAt   int64  `json:"lastOpenedAt"`
}

// AgentSessionCreate is Create's input: an id, CreatedAt and LastOpenedAt are
// assigned by the store.
type AgentSessionCreate struct {
	Workspace      string
	Name           string
	Agent          string
	AgentSessionID string
}

func mapAgentSessionFromDB(row queries.AgentWorkspaceSession) AgentSession {
	return AgentSession{
		ID: row.ID, Workspace: row.Workspace, Name: row.Name, Agent: row.Agent,
		AgentSessionID: row.AgentSessionID, CreatedAt: row.CreatedAt, LastOpenedAt: row.LastOpenedAt,
	}
}
