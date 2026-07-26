package main

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

func TestCommandRejectsInvalidLogLevel(t *testing.T) {
	logger := zerolog.Nop()
	err := newDevtoolsCommand(&logger).Run(context.Background(), []string{"devtools", "--log-level", "chatty", "prepare"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Unknown Level String")
}

func TestCommandRejectsPositionalArguments(t *testing.T) {
	logger := zerolog.Nop()
	err := newDevtoolsCommand(&logger).Run(context.Background(), []string{"devtools", "prepare", "unexpected"})
	require.Error(t, err)
	var exit cli.ExitCoder
	require.ErrorAs(t, err, &exit)
	assert.Equal(t, 2, exit.ExitCode())
}
