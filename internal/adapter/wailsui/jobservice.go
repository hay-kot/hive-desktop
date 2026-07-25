package wailsui

import (
	"context"

	"github.com/hay-kot/hive-desktop/internal/app"
	"github.com/hay-kot/hive-desktop/internal/app/jobs"
)

// JobService exposes live action-run jobs. The titlebar reads active and
// briefly lingering jobs through ListActive; List pages history.
type JobService struct {
	jobs *app.JobService
}

func NewJobService(j *app.JobService) *JobService { return &JobService{jobs: j} }

// List returns up to limit jobs with id < before, newest first.
func (s *JobService) List(before int64, limit int) ([]jobs.Job, error) {
	return s.jobs.List(context.Background(), before, limit)
}

// ListActive returns non-terminal jobs plus terminal jobs completed within
// the lingering window.
func (s *JobService) ListActive() ([]jobs.Job, error) {
	return s.jobs.ListActive(context.Background())
}
