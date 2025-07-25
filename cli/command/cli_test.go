package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/config/configfile"
	"github.com/docker/cli/cli/flags"
	"github.com/moby/moby/api/types"
	"github.com/moby/moby/client"
	"gotest.tools/v3/assert"
)

func TestNewAPIClientFromFlags(t *testing.T) {
	host := "unix://path"
	if runtime.GOOS == "windows" {
		host = "npipe://./"
	}
	opts := &flags.ClientOptions{Hosts: []string{host}}
	apiClient, err := NewAPIClientFromFlags(opts, &configfile.ConfigFile{})
	assert.NilError(t, err)
	assert.Equal(t, apiClient.DaemonHost(), host)
	assert.Equal(t, apiClient.ClientVersion(), client.DefaultAPIVersion)
}

func TestNewAPIClientFromFlagsForDefaultSchema(t *testing.T) {
	host := ":2375"
	slug := "tcp://localhost"
	if runtime.GOOS == "windows" {
		slug = "tcp://127.0.0.1"
	}
	opts := &flags.ClientOptions{Hosts: []string{host}}
	apiClient, err := NewAPIClientFromFlags(opts, &configfile.ConfigFile{})
	assert.NilError(t, err)
	assert.Equal(t, apiClient.DaemonHost(), slug+host)
	assert.Equal(t, apiClient.ClientVersion(), client.DefaultAPIVersion)
}

func TestNewAPIClientFromFlagsWithCustomHeaders(t *testing.T) {
	var received map[string]string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = map[string]string{
			"My-Header":  r.Header.Get("My-Header"),
			"User-Agent": r.Header.Get("User-Agent"),
		}
		_, _ = w.Write([]byte("OK"))
	}))
	defer ts.Close()
	host := strings.Replace(ts.URL, "http://", "tcp://", 1)
	opts := &flags.ClientOptions{Hosts: []string{host}}
	configFile := &configfile.ConfigFile{
		HTTPHeaders: map[string]string{
			"My-Header": "Custom-Value",
		},
	}

	apiClient, err := NewAPIClientFromFlags(opts, configFile)
	assert.NilError(t, err)
	assert.Equal(t, apiClient.DaemonHost(), host)
	assert.Equal(t, apiClient.ClientVersion(), client.DefaultAPIVersion)

	// verify User-Agent is not appended to the configfile. see https://github.com/docker/cli/pull/2756
	assert.DeepEqual(t, configFile.HTTPHeaders, map[string]string{"My-Header": "Custom-Value"})

	expectedHeaders := map[string]string{
		"My-Header":  "Custom-Value",
		"User-Agent": UserAgent(),
	}
	_, err = apiClient.Ping(context.Background())
	assert.NilError(t, err)
	assert.DeepEqual(t, received, expectedHeaders)
}

func TestNewAPIClientFromFlagsWithCustomHeadersFromEnv(t *testing.T) {
	var received http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.Header.Clone()
		_, _ = w.Write([]byte("OK"))
	}))
	defer ts.Close()
	host := strings.Replace(ts.URL, "http://", "tcp://", 1)
	opts := &flags.ClientOptions{Hosts: []string{host}}
	configFile := &configfile.ConfigFile{
		HTTPHeaders: map[string]string{
			"My-Header": "Custom-Value from config-file",
		},
	}

	// envOverrideHTTPHeaders should override the HTTPHeaders from the config-file,
	// so "My-Header" should not be present.
	t.Setenv(envOverrideHTTPHeaders, `one=one-value,"two=two,value",three=,four=four-value,four=four-value-override`)
	apiClient, err := NewAPIClientFromFlags(opts, configFile)
	assert.NilError(t, err)
	assert.Equal(t, apiClient.DaemonHost(), host)
	assert.Equal(t, apiClient.ClientVersion(), client.DefaultAPIVersion)

	expectedHeaders := http.Header{
		"One":        []string{"one-value"},
		"Two":        []string{"two,value"},
		"Three":      []string{""},
		"Four":       []string{"four-value-override"},
		"User-Agent": []string{UserAgent()},
	}
	_, err = apiClient.Ping(context.Background())
	assert.NilError(t, err)
	assert.DeepEqual(t, received, expectedHeaders)
}

func TestNewAPIClientFromFlagsWithAPIVersionFromEnv(t *testing.T) {
	const customVersion = "v3.3.3"
	const expectedVersion = "3.3.3"
	t.Setenv("DOCKER_API_VERSION", customVersion)
	t.Setenv("DOCKER_HOST", ":2375")

	opts := &flags.ClientOptions{}
	configFile := &configfile.ConfigFile{}
	apiclient, err := NewAPIClientFromFlags(opts, configFile)
	assert.NilError(t, err)
	assert.Equal(t, apiclient.ClientVersion(), expectedVersion)
}

type fakeClient struct {
	client.Client
	pingFunc   func() (types.Ping, error)
	version    string
	negotiated bool
}

func (c *fakeClient) Ping(_ context.Context) (types.Ping, error) {
	return c.pingFunc()
}

func (c *fakeClient) ClientVersion() string {
	return c.version
}

func (c *fakeClient) NegotiateAPIVersionPing(types.Ping) {
	c.negotiated = true
}

func TestInitializeFromClient(t *testing.T) {
	const defaultVersion = "v1.55"

	testcases := []struct {
		doc            string
		pingFunc       func() (types.Ping, error)
		expectedServer ServerInfo
		negotiated     bool
	}{
		{
			doc: "successful ping",
			pingFunc: func() (types.Ping, error) {
				return types.Ping{Experimental: true, OSType: "linux", APIVersion: "v1.30"}, nil
			},
			expectedServer: ServerInfo{HasExperimental: true, OSType: "linux"},
			negotiated:     true,
		},
		{
			doc: "failed ping, no API version",
			pingFunc: func() (types.Ping, error) {
				return types.Ping{}, errors.New("failed")
			},
			expectedServer: ServerInfo{HasExperimental: true},
		},
		{
			doc: "failed ping, with API version",
			pingFunc: func() (types.Ping, error) {
				return types.Ping{APIVersion: "v1.33"}, errors.New("failed")
			},
			expectedServer: ServerInfo{HasExperimental: true},
			negotiated:     true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.doc, func(t *testing.T) {
			apiclient := &fakeClient{
				pingFunc: tc.pingFunc,
				version:  defaultVersion,
			}

			cli := &DockerCli{client: apiclient}
			err := cli.Initialize(flags.NewClientOptions())
			assert.NilError(t, err)
			assert.DeepEqual(t, cli.ServerInfo(), tc.expectedServer)
			assert.Equal(t, apiclient.negotiated, tc.negotiated)
		})
	}
}

// Makes sure we don't hang forever on the initial connection.
// https://github.com/docker/cli/issues/3652
func TestInitializeFromClientHangs(t *testing.T) {
	tmpDir := t.TempDir()
	socket := filepath.Join(tmpDir, "my.sock")
	l, err := net.Listen("unix", socket)
	assert.NilError(t, err)

	receiveReqCh := make(chan bool)
	timeoutCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// Simulate a server that hangs on connections.
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-timeoutCtx.Done():
		case receiveReqCh <- true: // Blocks until someone receives on the channel.
		}
		_, _ = w.Write([]byte("OK"))
	}))
	ts.Listener = l
	ts.Start()
	defer ts.Close()

	opts := &flags.ClientOptions{Hosts: []string{"unix://" + socket}}
	configFile := &configfile.ConfigFile{}
	apiClient, err := NewAPIClientFromFlags(opts, configFile)
	assert.NilError(t, err)

	initializedCh := make(chan bool)

	go func() {
		cli := &DockerCli{client: apiClient, initTimeout: time.Millisecond}
		err := cli.Initialize(flags.NewClientOptions())
		assert.Check(t, err)
		cli.CurrentVersion()
		close(initializedCh)
	}()

	select {
	case <-timeoutCtx.Done():
		t.Fatal("timeout waiting for initialization to complete")
	case <-initializedCh:
	}

	select {
	case <-timeoutCtx.Done():
		t.Fatal("server never received an init request")
	case <-receiveReqCh:
	}
}

func TestNewDockerCliAndOperators(t *testing.T) {
	// Test default operations and also overriding default ones
	cli, err := NewDockerCli(WithInputStream(io.NopCloser(strings.NewReader("some input"))))
	assert.NilError(t, err)
	// Check streams are initialized
	assert.Check(t, cli.In() != nil)
	assert.Check(t, cli.Out() != nil)
	assert.Check(t, cli.Err() != nil)
	inputStream, err := io.ReadAll(cli.In())
	assert.NilError(t, err)
	assert.Equal(t, string(inputStream), "some input")

	// Apply can modify a dockerCli after construction
	outbuf := bytes.NewBuffer(nil)
	errbuf := bytes.NewBuffer(nil)
	err = cli.Apply(
		WithInputStream(io.NopCloser(strings.NewReader("input"))),
		WithOutputStream(outbuf),
		WithErrorStream(errbuf),
	)
	assert.NilError(t, err)
	// Check input stream
	inputStream, err = io.ReadAll(cli.In())
	assert.NilError(t, err)
	assert.Equal(t, string(inputStream), "input")
	// Check output stream
	_, err = fmt.Fprint(cli.Out(), "output")
	assert.NilError(t, err)
	outputStream, err := io.ReadAll(outbuf)
	assert.NilError(t, err)
	assert.Equal(t, string(outputStream), "output")
	// Check error stream
	_, err = fmt.Fprint(cli.Err(), "error")
	assert.NilError(t, err)
	errStream, err := io.ReadAll(errbuf)
	assert.NilError(t, err)
	assert.Equal(t, string(errStream), "error")
}

func TestInitializeShouldAlwaysCreateTheContextStore(t *testing.T) {
	cli, err := NewDockerCli()
	assert.NilError(t, err)
	assert.NilError(t, cli.Initialize(flags.NewClientOptions(), WithInitializeClient(func(cli *DockerCli) (client.APIClient, error) {
		return client.NewClientWithOpts()
	})))
	assert.Check(t, cli.ContextStore() != nil)
}

func TestHooksEnabled(t *testing.T) {
	t.Run("disabled by default", func(t *testing.T) {
		// Make sure we don't depend on any existing ~/.docker/config.json
		config.SetDir(t.TempDir())
		cli, err := NewDockerCli()
		assert.NilError(t, err)

		assert.Check(t, !cli.HooksEnabled())
	})

	t.Run("enabled in configFile", func(t *testing.T) {
		configFile := `{
    "features": {
      "hooks": "true"
    }}`
		config.SetDir(t.TempDir())
		err := os.WriteFile(filepath.Join(config.Dir(), "config.json"), []byte(configFile), 0o600)
		assert.NilError(t, err)
		cli, err := NewDockerCli()
		assert.NilError(t, err)
		assert.Check(t, cli.HooksEnabled())
	})

	t.Run("env var overrides configFile", func(t *testing.T) {
		configFile := `{
    "features": {
      "hooks": "true"
    }}`
		t.Setenv("DOCKER_CLI_HOOKS", "false")
		config.SetDir(t.TempDir())
		err := os.WriteFile(filepath.Join(config.Dir(), "config.json"), []byte(configFile), 0o600)
		assert.NilError(t, err)
		cli, err := NewDockerCli()
		assert.NilError(t, err)
		assert.Check(t, !cli.HooksEnabled())
	})

	t.Run("legacy env var overrides configFile", func(t *testing.T) {
		configFile := `{
    "features": {
      "hooks": "true"
    }}`
		t.Setenv("DOCKER_CLI_HINTS", "false")
		config.SetDir(t.TempDir())
		err := os.WriteFile(filepath.Join(config.Dir(), "config.json"), []byte(configFile), 0o600)
		assert.NilError(t, err)
		cli, err := NewDockerCli()
		assert.NilError(t, err)
		assert.Check(t, !cli.HooksEnabled())
	})
}
