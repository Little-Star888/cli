package swarm

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/docker/cli/internal/test"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/api/types/system"
	"gotest.tools/v3/assert"
	is "gotest.tools/v3/assert/cmp"
)

func TestSwarmJoinErrors(t *testing.T) {
	testCases := []struct {
		name          string
		args          []string
		swarmJoinFunc func() error
		infoFunc      func() (system.Info, error)
		expectedError string
	}{
		{
			name:          "not-enough-args",
			args:          []string{},
			expectedError: "requires 1 argument",
		},
		{
			name:          "too-many-args",
			args:          []string{"remote1", "remote2"},
			expectedError: "requires 1 argument",
		},
		{
			name: "join-failed",
			args: []string{"remote"},
			swarmJoinFunc: func() error {
				return errors.New("error joining the swarm")
			},
			expectedError: "error joining the swarm",
		},
		{
			name: "join-failed-on-init",
			args: []string{"remote"},
			infoFunc: func() (system.Info, error) {
				return system.Info{}, errors.New("error asking for node info")
			},
			expectedError: "error asking for node info",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newJoinCommand(
				test.NewFakeCli(&fakeClient{
					swarmJoinFunc: tc.swarmJoinFunc,
					infoFunc:      tc.infoFunc,
				}))
			cmd.SetArgs(tc.args)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			assert.ErrorContains(t, cmd.Execute(), tc.expectedError)
		})
	}
}

func TestSwarmJoin(t *testing.T) {
	testCases := []struct {
		name     string
		infoFunc func() (system.Info, error)
		expected string
	}{
		{
			name: "join-as-manager",
			infoFunc: func() (system.Info, error) {
				return system.Info{
					Swarm: swarm.Info{
						ControlAvailable: true,
					},
				}, nil
			},
			expected: "This node joined a swarm as a manager.",
		},
		{
			name: "join-as-worker",
			infoFunc: func() (system.Info, error) {
				return system.Info{
					Swarm: swarm.Info{
						ControlAvailable: false,
					},
				}, nil
			},
			expected: "This node joined a swarm as a worker.",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cli := test.NewFakeCli(&fakeClient{
				infoFunc: tc.infoFunc,
			})
			cmd := newJoinCommand(cli)
			cmd.SetArgs([]string{"remote"})
			assert.NilError(t, cmd.Execute())
			assert.Check(t, is.Equal(strings.TrimSpace(cli.OutBuffer().String()), tc.expected))
		})
	}
}
