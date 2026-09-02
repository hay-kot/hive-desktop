// Package schedule launches recurring agent chats. A Spec is one entry in a
// workspace manifest's schedules: list; a Run is one execution of it.
//
// The package is a leaf on purpose: cron parsing, prompt templating, the
// evaluation planner, and the loop that drives them are all testable without
// tmux, a database, or the agentws package that imports this one. Everything
// the Scheduler needs from the rest of the app arrives through the ports
// declared in scheduler.go.
//
// A missed occurrence is what the cursor exists for. Evaluate looks at the
// window between the cursor and now rather than at a timer, so a schedule due
// while the app was closed still fires on the next launch instead of being
// lost.
package schedule
