# Admin Server

Placeholder. Future Go backend for analytics, licenses, and purchases. Design is
a separate doc before any code lands.

It arrives as a nested Go module (`github.com/hay-kot/hive-desktop/server`) with
its own deploy workflow, deliberately outside the root module so the deployed
service does not carry wails/charm dependencies; a root `go.work` is added at
that point. Shared wire types (analytics events, license payloads) go in a
`shared/` nested module if needed.
