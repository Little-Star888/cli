package service

import (
	"encoding/hex"

	"github.com/distribution/reference"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/trust"
	"github.com/docker/cli/internal/registry"
	"github.com/moby/moby/api/types/swarm"
	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/theupdateframework/notary/tuf/data"
)

func resolveServiceImageDigestContentTrust(dockerCli command.Cli, service *swarm.ServiceSpec) error {
	if !dockerCli.ContentTrustEnabled() {
		// When not using content trust, digest resolution happens later when
		// contacting the registry to retrieve image information.
		return nil
	}

	ref, err := reference.ParseAnyReference(service.TaskTemplate.ContainerSpec.Image)
	if err != nil {
		return errors.Wrapf(err, "invalid reference %s", service.TaskTemplate.ContainerSpec.Image)
	}

	// If reference does not have digest (is not canonical nor image id)
	if _, ok := ref.(reference.Digested); !ok {
		namedRef, ok := ref.(reference.Named)
		if !ok {
			return errors.New("failed to resolve image digest using content trust: reference is not named")
		}
		namedRef = reference.TagNameOnly(namedRef)
		taggedRef, ok := namedRef.(reference.NamedTagged)
		if !ok {
			return errors.New("failed to resolve image digest using content trust: reference is not tagged")
		}

		resolvedImage, err := trustedResolveDigest(dockerCli, taggedRef)
		if err != nil {
			return errors.Wrap(err, "failed to resolve image digest using content trust")
		}
		resolvedFamiliar := reference.FamiliarString(resolvedImage)
		logrus.Debugf("resolved image tag to %s using content trust", resolvedFamiliar)
		service.TaskTemplate.ContainerSpec.Image = resolvedFamiliar
	}

	return nil
}

func trustedResolveDigest(cli command.Cli, ref reference.NamedTagged) (reference.Canonical, error) {
	repoInfo := registry.ParseRepositoryInfo(ref)
	authConfig := command.ResolveAuthConfig(cli.ConfigFile(), repoInfo.Index)

	notaryRepo, err := trust.GetNotaryRepository(cli.In(), cli.Out(), command.UserAgent(), repoInfo, &authConfig, "pull")
	if err != nil {
		return nil, errors.Wrap(err, "error establishing connection to trust repository")
	}

	t, err := notaryRepo.GetTargetByName(ref.Tag(), trust.ReleasesRole, data.CanonicalTargetsRole)
	if err != nil {
		return nil, trust.NotaryError(repoInfo.Name.Name(), err)
	}
	// Only get the tag if it's in the top level targets role or the releases delegation role
	// ignore it if it's in any other delegation roles
	if t.Role != trust.ReleasesRole && t.Role != data.CanonicalTargetsRole {
		return nil, trust.NotaryError(repoInfo.Name.Name(), errors.Errorf("No trust data for %s", reference.FamiliarString(ref)))
	}

	logrus.Debugf("retrieving target for %s role", t.Role)
	h, ok := t.Hashes["sha256"]
	if !ok {
		return nil, errors.New("no valid hash, expecting sha256")
	}

	dgst := digest.NewDigestFromHex("sha256", hex.EncodeToString(h))

	// Allow returning canonical reference with tag
	return reference.WithDigest(ref, dgst)
}
