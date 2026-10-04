package runner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
)

func TestCapabilitiesDiscoverExecutableToolsAndIgnoreLabels(t *testing.T) {
	cfg := RunnerConfig{Timeout: 42 * time.Second, WorkingDirectory: "/worker", CustomLabels: manifest.Labels{
		urth.LabelWorkerOS: "forged", LabelNodeJsVersion: "999.0.0", kindAsLabel("puppeteer"): "999.0.0",
	}}
	command := func(_ context.Context, dir, name string, args ...string) ([]byte, error) {
		switch name {
		case "node":
			if args[0] == "-e" {
				require.Equal(t, "/worker", dir)
				return nil, errors.New("browser missing")
			}
			return []byte("v1.10.0\n"), nil
		case "python3":
			return []byte("Python development-build\n"), nil
		}
		return nil, errors.New("not installed")
	}
	caps := cfg.discoverCapabilities(context.Background(), command, func() (bool, bool) { return false, false })
	require.NotEqual(t, "forged", caps.OS)
	require.Equal(t, "1.10.0", caps.RuntimeVersions["node"])
	require.Equal(t, "development-build", caps.RuntimeVersions["python"])
	require.NotContains(t, caps.RuntimeVersions, "npm")
	require.NotContains(t, caps.ProbeVersions, "puppeteer")
	require.NotContains(t, caps.ProbeVersions, "icmp")
	require.Empty(t, caps.Privileges)
	require.Equal(t, "42s", caps.MaxDuration)
	require.Equal(t, "1ns", caps.MinDuration)
	for kind, registration := range prob.ListProbs() {
		if kind == "icmp" || strings.Contains(string(kind), "puppeteer") {
			continue
		}
		require.Equal(t, registration.Version, caps.ProbeVersions[string(kind)])
	}
}

func TestCapabilitiesBrowserRequiresInstalledPackagesAndExecutable(t *testing.T) {
	if !browserProfile {
		t.Skip("browser runtime excluded from native profile")
	}
	cfg := RunnerConfig{WorkingDirectory: "/worker"}
	command := func(_ context.Context, dir, name string, args ...string) ([]byte, error) {
		if name == "node" && args[0] == "-e" {
			require.Equal(t, "/worker", dir)
			require.Contains(t, args[1], "fs.constants.X_OK")
			require.Contains(t, args[1], "puppeteer-har")
			return []byte(`{"puppeteer":"24.1.0","har":"3.0.0","path":"/chrome"}`), nil
		}
		if name == "node" {
			return []byte("v24.0.0"), nil
		}
		if name == "/chrome" {
			require.Equal(t, []string{"--version"}, args)
			return []byte("Google Chrome for Testing 149.0.7754.0\n"), nil
		}
		if name == "npm" {
			return []byte("11.0.0"), nil
		}
		return nil, errors.New("absent")
	}
	caps := cfg.discoverCapabilities(context.Background(), command, func() (bool, bool) { return true, true })
	require.Contains(t, caps.ProbeVersions, "puppeteer")
	require.Equal(t, "24.1.0", caps.RuntimeVersions["puppeteer"])
	require.Equal(t, "3.0.0", caps.RuntimeVersions["puppeteer-har"])
	require.Equal(t, "149.0.7754.0", caps.RuntimeVersions["browser"])
	require.Equal(t, "11.0.0", caps.RuntimeVersions["npm"])
	require.Equal(t, []string{"raw-sockets"}, caps.Privileges)
	require.Contains(t, caps.ProbeVersions, "icmp")
	require.Equal(t, "1m0s", caps.MaxDuration)
}

func TestCapabilitiesUnprivilegedICMPAndMissingBrowser(t *testing.T) {
	cfg := RunnerConfig{}
	caps := cfg.discoverCapabilities(context.Background(), func(context.Context, string, string, ...string) ([]byte, error) {
		return nil, errors.New("not installed")
	}, func() (bool, bool) { return true, false })
	require.Contains(t, caps.ProbeVersions, "icmp")
	require.NotContains(t, caps.ProbeVersions, "puppeteer")
	require.Empty(t, caps.Privileges)
	require.Empty(t, caps.RuntimeVersions)
}

func TestBrowserVersionPreservesRawVersion(t *testing.T) {
	for raw, want := range map[string]string{
		"Google Chrome for Testing 149.0.7754.0\n": "149.0.7754.0",
		"Chromium 1.10.0":                          "1.10.0",
		"Chromium development-build":               "Chromium development-build",
		"Chromium 149.0.7754.0-custom":             "149.0.7754.0-custom",
	} {
		require.Equal(t, want, browserVersion(raw))
	}
}

func TestBrowserMissingDependencyDoesNotAdvertiseExecutableProbe(t *testing.T) {
	if !browserProfile {
		t.Skip("browser runtime excluded from native profile")
	}
	for _, missing := range []string{"npm", "package", "browser"} {
		t.Run(missing, func(t *testing.T) {
			cfg := RunnerConfig{WorkingDirectory: "/worker"}
			caps := cfg.discoverCapabilities(context.Background(), func(_ context.Context, _ string, name string, args ...string) ([]byte, error) {
				switch name {
				case "node":
					if args[0] == "-e" {
						if missing == "package" {
							return nil, errors.New("package absent")
						}
						return []byte(`{"puppeteer":"24.1.0","har":"3.0.0","path":"/chrome"}`), nil
					}
					return []byte("v24.0.0"), nil
				case "npm":
					if missing == "npm" {
						return nil, errors.New("npm absent")
					}
					return []byte("11.0.0"), nil
				case "/chrome":
					return nil, errors.New("browser cannot execute")
				}
				return nil, errors.New("absent")
			}, func() (bool, bool) { return false, false })
			require.NotContains(t, caps.ProbeVersions, "puppeteer")
			require.NotContains(t, caps.RuntimeVersions, "browser")
			if missing == "npm" {
				require.NotContains(t, caps.RuntimeVersions, "npm")
			}
			if missing == "browser" {
				require.Equal(t, "24.1.0", caps.RuntimeVersions["puppeteer"])
			}
		})
	}
}
