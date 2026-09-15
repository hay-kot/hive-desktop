package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/hay-kot/hive-desktop/internal/app/actions"
	"github.com/hay-kot/hive-desktop/internal/app/activity"
	"github.com/hay-kot/hive-desktop/internal/app/agentws"
	"github.com/hay-kot/hive-desktop/internal/app/configstate"
	"github.com/hay-kot/hive-desktop/internal/app/configwatch"
	"github.com/hay-kot/hive-desktop/internal/app/events"
	"github.com/hay-kot/hive-desktop/internal/app/flow"
	"github.com/hay-kot/hive-desktop/internal/app/settings"
)

type configurationDetector interface {
	Start(context.Context) error
	Next(context.Context) (configwatch.Batch, error)
	Ack(context.Context, configwatch.Batch)
	Status(context.Context) []configwatch.Status
	Stop(context.Context) error
}

type configuration struct {
	app      *App
	detector configurationDetector
	logger   zerolog.Logger

	cancel      context.CancelFunc
	done        chan struct{}
	loopStarted bool
	once        sync.Once

	applySource func(context.Context, configstate.Source) error
	publish     func(context.Context, events.Event)
}

func newConfiguration(app *App) (*configuration, error) {
	manager, err := configwatch.New(configwatch.Options{Logger: app.logger})
	if err != nil {
		return nil, fmt.Errorf("create configuration manager: %w", err)
	}
	for _, registration := range []configwatch.Registration{
		{Source: configstate.Settings, Authority: settings.NewAuthority(app.paths.SettingsPath)},
		{Source: configstate.Actions, Authority: actions.NewAuthority(app.paths.ActionsPath)},
		{Source: configstate.Flows, Authority: flow.NewAuthority(app.paths.FlowsDir)},
		{Source: configstate.AgentWorkspaces, Authority: agentws.NewAuthority(app.paths.AgentWorkspacesDir)},
	} {
		if err := manager.Register(app.ctx, registration); err != nil {
			return nil, fmt.Errorf("register %s configuration authority: %w", registration.Source, err)
		}
	}
	return &configuration{app: app, detector: manager, logger: app.logger, done: make(chan struct{})}, nil
}

func (c *configuration) begin(ctx context.Context, cancel context.CancelFunc) error {
	if err := ctx.Err(); err != nil {
		cancel()
		return err
	}
	c.cancel = cancel
	if err := c.detector.Start(ctx); err != nil {
		cancel()
		return err
	}
	return nil
}

func (c *configuration) start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.loopStarted = true
	go func() {
		defer close(c.done)
		c.run(ctx)
	}()
	return nil
}

func (c *configuration) stop(ctx context.Context) error {
	c.once.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
	})

	managerDone := make(chan error, 1)
	go func() { managerDone <- c.detector.Stop(ctx) }()
	var loopDone <-chan struct{}
	if c.loopStarted {
		loopDone = c.done
	}
	for managerDone != nil || loopDone != nil {
		select {
		case err := <-managerDone:
			if err != nil {
				return err
			}
			managerDone = nil
		case <-loopDone:
			loopDone = nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (c *configuration) status(ctx context.Context) []configwatch.Status {
	return c.detector.Status(ctx)
}

func (c *configuration) run(ctx context.Context) {
	for {
		batch, err := c.detector.Next(ctx)
		if err != nil {
			if !errors.Is(err, configwatch.ErrStopped) && ctx.Err() == nil {
				c.logger.Warn().Err(err).Msg("configuration reconciliation stopped")
			}
			return
		}
		processed := c.reconcile(ctx, batch)
		c.detector.Ack(ctx, processed)
	}
}

func (c *configuration) reconcile(ctx context.Context, batch configwatch.Batch) configwatch.Batch {
	processed := configwatch.Batch{ID: batch.ID}
	bySource := make(map[configstate.Source]configwatch.Dirty, len(batch.Dirty))
	for _, dirty := range batch.Dirty {
		bySource[dirty.Source] = dirty
	}

	var actionsUpdated bool
	var flowsUpdated bool
	var workspacesUpdated bool
	for sourceIndex, source := range []configstate.Source{
		configstate.Settings,
		configstate.Actions,
		configstate.Flows,
		configstate.AgentWorkspaces,
	} {
		dirty, ok := bySource[source]
		if !ok || ctx.Err() != nil {
			continue
		}
		startedAt := time.Now()
		err := c.apply(ctx, source)
		c.logReconcile(ctx, batch.ID, sourceIndex, dirty, time.Now(), time.Since(startedAt), err)
		switch source {
		case configstate.Settings:
		case configstate.Actions:
			actionsUpdated = true
		case configstate.Flows:
			flowsUpdated = true
		case configstate.AgentWorkspaces:
			workspacesUpdated = true
		}
		processed.Dirty = append(processed.Dirty, dirty)
	}

	if flowsUpdated {
		c.app.engine.Reload()
	}
	if workspacesUpdated {
		c.app.scheduler.Reload()
	}
	if actionsUpdated {
		count := len(c.app.actionStore.List())
		c.publishEvent(ctx, events.ActionsUpdated{Count: count})
		c.app.Activity.Record(ctx, activity.ConfigReloaded("actions.yml", count))
	}
	if flowsUpdated {
		c.publishEvent(ctx, events.FlowsUpdated{Reason: "reload"})
	}
	if workspacesUpdated {
		count := len(c.app.agentWorkspaceStore.Statuses())
		c.publishEvent(ctx, events.AgentWorkspacesUpdated{Count: count})
	}
	return processed
}

func (c *configuration) apply(ctx context.Context, source configstate.Source) error {
	if c.applySource != nil {
		return c.applySource(ctx, source)
	}

	switch source {
	case configstate.Settings:
		_, err := c.app.settingsStore.Effective()
		return err
	case configstate.Actions:
		return c.app.actionStore.Reload()
	case configstate.Flows:
		return c.app.flowStore.Reload()
	case configstate.AgentWorkspaces:
		return c.app.agentWorkspaceStore.Reload()
	default:
		return nil
	}
}

func (c *configuration) logReconcile(ctx context.Context, batchID uint64, sourceIndex int, dirty configwatch.Dirty, completedAt time.Time, duration time.Duration, err error) {
	level := zerolog.InfoLevel
	outcome := "applied"
	if err != nil {
		level = zerolog.WarnLevel
		outcome = "error"
	}
	c.logger.WithLevel(level).
		Ctx(ctx).
		Uint64("batch_id", batchID).
		Int("source_index", sourceIndex).
		Str("source", string(dirty.Source)).
		Str("trigger", string(dirty.Trigger)).
		Str("observed_revision", string(dirty.ObservedRevision)).
		Time("completed_at", completedAt).
		Int64("duration_ms", duration.Milliseconds()).
		Str("outcome", outcome).
		Msg("configuration source reconciled")
}

func (c *configuration) publishEvent(ctx context.Context, event events.Event) {
	if c.publish != nil {
		c.publish(ctx, event)
		return
	}
	c.app.Events.Publish(ctx, event)
}
