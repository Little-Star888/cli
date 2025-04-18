package manifest

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/distribution/reference"
	"github.com/docker/cli/cli/manifest/store"
	"github.com/docker/cli/cli/manifest/types"
	"github.com/docker/cli/internal/test"
	"github.com/docker/distribution"
	"github.com/docker/distribution/manifest/schema2"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"gotest.tools/v3/assert"
	is "gotest.tools/v3/assert/cmp"
	"gotest.tools/v3/golden"
)

func ref(t *testing.T, name string) reference.Named {
	t.Helper()
	named, err := reference.ParseNamed("example.com/" + name)
	assert.NilError(t, err)
	return named
}

func fullImageManifest(t *testing.T, ref reference.Named) types.ImageManifest {
	t.Helper()
	man, err := schema2.FromStruct(schema2.Manifest{
		Versioned: schema2.SchemaVersion,
		Config: distribution.Descriptor{
			Digest:    "sha256:7328f6f8b41890597575cbaadc884e7386ae0acc53b747401ebce5cf0d624560",
			Size:      1520,
			MediaType: schema2.MediaTypeImageConfig,
		},
		Layers: []distribution.Descriptor{
			{
				MediaType: schema2.MediaTypeLayer,
				Size:      1990402,
				Digest:    "sha256:88286f41530e93dffd4b964e1db22ce4939fffa4a4c665dab8591fbab03d4926",
			},
		},
	})
	assert.NilError(t, err)

	// TODO: include image data for verbose inspect
	mt, raw, err := man.Payload()
	assert.NilError(t, err)

	desc := ocispec.Descriptor{
		Digest:    digest.FromBytes(raw),
		Size:      int64(len(raw)),
		MediaType: mt,
		Platform: &ocispec.Platform{
			Architecture: "amd64",
			OS:           "linux",
		},
	}

	return types.NewImageManifest(ref, desc, man)
}

func TestInspectCommandLocalManifestNotFound(t *testing.T) {
	refStore := store.NewStore(t.TempDir())

	cli := test.NewFakeCli(nil)
	cli.SetManifestStore(refStore)

	cmd := newInspectCommand(cli)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"example.com/list:v1", "example.com/alpine:3.0"})
	err := cmd.Execute()
	assert.Error(t, err, "No such manifest: example.com/alpine:3.0")
}

func TestInspectCommandNotFound(t *testing.T) {
	refStore := store.NewStore(t.TempDir())

	cli := test.NewFakeCli(nil)
	cli.SetManifestStore(refStore)
	cli.SetRegistryClient(&fakeRegistryClient{
		getManifestFunc: func(_ context.Context, _ reference.Named) (types.ImageManifest, error) {
			return types.ImageManifest{}, errors.New("missing")
		},
		getManifestListFunc: func(ctx context.Context, ref reference.Named) ([]types.ImageManifest, error) {
			return nil, errors.New("No such manifest: " + ref.String())
		},
	})

	cmd := newInspectCommand(cli)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"example.com/alpine:3.0"})
	err := cmd.Execute()
	assert.Error(t, err, "No such manifest: example.com/alpine:3.0")
}

func TestInspectCommandLocalManifest(t *testing.T) {
	refStore := store.NewStore(t.TempDir())

	cli := test.NewFakeCli(nil)
	cli.SetManifestStore(refStore)
	namedRef := ref(t, "alpine:3.0")
	imageManifest := fullImageManifest(t, namedRef)
	err := refStore.Save(ref(t, "list:v1"), namedRef, imageManifest)
	assert.NilError(t, err)

	cmd := newInspectCommand(cli)
	cmd.SetArgs([]string{"example.com/list:v1", "example.com/alpine:3.0"})
	assert.NilError(t, cmd.Execute())
	actual := cli.OutBuffer()
	expected := golden.Get(t, "inspect-manifest.golden")
	assert.Check(t, is.Equal(string(expected), actual.String()))
}

func TestInspectcommandRemoteManifest(t *testing.T) {
	refStore := store.NewStore(t.TempDir())

	cli := test.NewFakeCli(nil)
	cli.SetManifestStore(refStore)
	cli.SetRegistryClient(&fakeRegistryClient{
		getManifestFunc: func(_ context.Context, ref reference.Named) (types.ImageManifest, error) {
			return fullImageManifest(t, ref), nil
		},
	})

	cmd := newInspectCommand(cli)
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{"example.com/alpine:3.0"})
	assert.NilError(t, cmd.Execute())
	actual := cli.OutBuffer()
	expected := golden.Get(t, "inspect-manifest.golden")
	assert.Check(t, is.Equal(string(expected), actual.String()))
}
