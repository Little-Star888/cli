package node

import (
	"context"
	"strings"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/command/completion"
	"github.com/docker/cli/cli/command/idresolver"
	"github.com/docker/cli/cli/command/task"
	"github.com/docker/cli/opts"
	"github.com/moby/moby/api/types/swarm"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type psOptions struct {
	nodeIDs   []string
	noResolve bool
	noTrunc   bool
	quiet     bool
	format    string
	filter    opts.FilterOpt
}

func newPsCommand(dockerCli command.Cli) *cobra.Command {
	options := psOptions{filter: opts.NewFilterOpt()}

	cmd := &cobra.Command{
		Use:   "ps [OPTIONS] [NODE...]",
		Short: "List tasks running on one or more nodes, defaults to current node",
		Args:  cli.RequiresMinArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.nodeIDs = []string{"self"}

			if len(args) != 0 {
				options.nodeIDs = args
			}

			return runPs(cmd.Context(), dockerCli, options)
		},
		ValidArgsFunction: completeNodeNames(dockerCli),
	}
	flags := cmd.Flags()
	flags.BoolVar(&options.noTrunc, "no-trunc", false, "Do not truncate output")
	flags.BoolVar(&options.noResolve, "no-resolve", false, "Do not map IDs to Names")
	flags.VarP(&options.filter, "filter", "f", "Filter output based on conditions provided")
	flags.StringVar(&options.format, "format", "", "Pretty-print tasks using a Go template")
	flags.BoolVarP(&options.quiet, "quiet", "q", false, "Only display task IDs")

	flags.VisitAll(func(flag *pflag.Flag) {
		// Set a default completion function if none was set. We don't look
		// up if it does already have one set, because Cobra does this for
		// us, and returns an error (which we ignore for this reason).
		_ = cmd.RegisterFlagCompletionFunc(flag.Name, completion.NoComplete)
	})
	return cmd
}

func runPs(ctx context.Context, dockerCli command.Cli, options psOptions) error {
	client := dockerCli.Client()

	var (
		errs  []string
		tasks []swarm.Task
	)

	for _, nodeID := range options.nodeIDs {
		nodeRef, err := Reference(ctx, client, nodeID)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}

		node, _, err := client.NodeInspectWithRaw(ctx, nodeRef)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}

		filter := options.filter.Value()
		filter.Add("node", node.ID)

		nodeTasks, err := client.TaskList(ctx, swarm.TaskListOptions{Filters: filter})
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}

		tasks = append(tasks, nodeTasks...)
	}

	format := options.format
	if len(format) == 0 {
		format = task.DefaultFormat(dockerCli.ConfigFile(), options.quiet)
	}

	if len(errs) == 0 || len(tasks) != 0 {
		if err := task.Print(ctx, dockerCli, tasks, idresolver.New(client, options.noResolve), !options.noTrunc, options.quiet, format); err != nil {
			errs = append(errs, err.Error())
		}
	}

	if len(errs) > 0 {
		return errors.Errorf("%s", strings.Join(errs, "\n"))
	}

	return nil
}
