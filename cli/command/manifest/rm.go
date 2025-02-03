package manifest

import (
	"context"
	"strings"

	"github.com/docker/cli/cli"
	"github.com/docker/cli/cli/command"
	manifeststore "github.com/docker/cli/cli/manifest/store"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

func newRmManifestListCommand(dockerCLI command.Cli) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rm MANIFEST_LIST [MANIFEST_LIST...]",
		Short: "Delete one or more manifest lists from local storage",
		Args:  cli.RequiresMinArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRemove(cmd.Context(), dockerCLI.ManifestStore(), args)
		},
	}

	return cmd
}

func runRemove(ctx context.Context, store manifeststore.Store, targets []string) error {
	var errs []string
	for _, target := range targets {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		targetRef, err := normalizeReference(target)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		_, err = store.GetList(targetRef)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		err = store.Remove(targetRef)
		if err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "\n"))
	}
	return nil
}
