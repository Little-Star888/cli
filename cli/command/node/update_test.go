package node

import (
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/docker/cli/internal/test"
	"github.com/docker/cli/internal/test/builders"
	"github.com/moby/moby/api/types/swarm"
	"gotest.tools/v3/assert"
)

func TestNodeUpdateErrors(t *testing.T) {
	testCases := []struct {
		args            []string
		flags           map[string]string
		nodeInspectFunc func() (swarm.Node, []byte, error)
		nodeUpdateFunc  func(nodeID string, version swarm.Version, node swarm.NodeSpec) error
		expectedError   string
	}{
		{
			expectedError: "requires 1 argument",
		},
		{
			args:          []string{"node1", "node2"},
			expectedError: "requires 1 argument",
		},
		{
			args: []string{"nodeID"},
			nodeInspectFunc: func() (swarm.Node, []byte, error) {
				return swarm.Node{}, []byte{}, errors.New("error inspecting the node")
			},
			expectedError: "error inspecting the node",
		},
		{
			args: []string{"nodeID"},
			nodeUpdateFunc: func(nodeID string, version swarm.Version, node swarm.NodeSpec) error {
				return errors.New("error updating the node")
			},
			expectedError: "error updating the node",
		},
		{
			args: []string{"nodeID"},
			nodeInspectFunc: func() (swarm.Node, []byte, error) {
				return *builders.Node(builders.NodeLabels(map[string]string{
					"key": "value",
				})), []byte{}, nil
			},
			flags: map[string]string{
				"label-rm": "notpresent",
			},
			expectedError: "key notpresent doesn't exist in node's labels",
		},
	}
	for _, tc := range testCases {
		cmd := newUpdateCommand(
			test.NewFakeCli(&fakeClient{
				nodeInspectFunc: tc.nodeInspectFunc,
				nodeUpdateFunc:  tc.nodeUpdateFunc,
			}))
		cmd.SetArgs(tc.args)
		for key, value := range tc.flags {
			assert.Check(t, cmd.Flags().Set(key, value))
		}
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		assert.ErrorContains(t, cmd.Execute(), tc.expectedError)
	}
}

func TestNodeUpdate(t *testing.T) {
	testCases := []struct {
		args            []string
		flags           map[string]string
		nodeInspectFunc func() (swarm.Node, []byte, error)
		nodeUpdateFunc  func(nodeID string, version swarm.Version, node swarm.NodeSpec) error
	}{
		{
			args: []string{"nodeID"},
			flags: map[string]string{
				"role": "manager",
			},
			nodeInspectFunc: func() (swarm.Node, []byte, error) {
				return *builders.Node(), []byte{}, nil
			},
			nodeUpdateFunc: func(nodeID string, version swarm.Version, node swarm.NodeSpec) error {
				if node.Role != swarm.NodeRoleManager {
					return errors.New("expected role manager, got " + string(node.Role))
				}
				return nil
			},
		},
		{
			args: []string{"nodeID"},
			flags: map[string]string{
				"availability": "drain",
			},
			nodeInspectFunc: func() (swarm.Node, []byte, error) {
				return *builders.Node(), []byte{}, nil
			},
			nodeUpdateFunc: func(nodeID string, version swarm.Version, node swarm.NodeSpec) error {
				if node.Availability != swarm.NodeAvailabilityDrain {
					return errors.New("expected drain availability, got " + string(node.Availability))
				}
				return nil
			},
		},
		{
			args: []string{"nodeID"},
			flags: map[string]string{
				"label-add": "lbl",
			},
			nodeInspectFunc: func() (swarm.Node, []byte, error) {
				return *builders.Node(), []byte{}, nil
			},
			nodeUpdateFunc: func(nodeID string, version swarm.Version, node swarm.NodeSpec) error {
				if _, present := node.Annotations.Labels["lbl"]; !present {
					return fmt.Errorf("expected 'lbl' label, got %v", node.Annotations.Labels)
				}
				return nil
			},
		},
		{
			args: []string{"nodeID"},
			flags: map[string]string{
				"label-add": "key=value",
			},
			nodeInspectFunc: func() (swarm.Node, []byte, error) {
				return *builders.Node(), []byte{}, nil
			},
			nodeUpdateFunc: func(nodeID string, version swarm.Version, node swarm.NodeSpec) error {
				if value, present := node.Annotations.Labels["key"]; !present || value != "value" {
					return fmt.Errorf("expected 'key' label to be 'value', got %v", node.Annotations.Labels)
				}
				return nil
			},
		},
		{
			args: []string{"nodeID"},
			flags: map[string]string{
				"label-rm": "key",
			},
			nodeInspectFunc: func() (swarm.Node, []byte, error) {
				return *builders.Node(builders.NodeLabels(map[string]string{
					"key": "value",
				})), []byte{}, nil
			},
			nodeUpdateFunc: func(nodeID string, version swarm.Version, node swarm.NodeSpec) error {
				if len(node.Annotations.Labels) > 0 {
					return fmt.Errorf("expected no labels, got %v", node.Annotations.Labels)
				}
				return nil
			},
		},
	}
	for _, tc := range testCases {
		cmd := newUpdateCommand(
			test.NewFakeCli(&fakeClient{
				nodeInspectFunc: tc.nodeInspectFunc,
				nodeUpdateFunc:  tc.nodeUpdateFunc,
			}))
		cmd.SetArgs(tc.args)
		for key, value := range tc.flags {
			assert.Check(t, cmd.Flags().Set(key, value))
		}
		assert.NilError(t, cmd.Execute())
	}
}
