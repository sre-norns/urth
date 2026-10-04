package runner

import (
	"context"
	"encoding/json"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/urth/pkg/urth"
	"golang.org/x/net/icmp"
)

type capabilityCommand func(context.Context, string, string, ...string) ([]byte, error)

// GetCapabilities discovers executable capabilities for each registration. Custom
// labels cannot add probes, runtimes, versions, privileges, or duration authority.
func (c *RunnerConfig) GetCapabilities(ctx context.Context) urth.WorkerCapabilities {
	return c.discoverCapabilities(ctx, runCapabilityCommand, discoverICMP)
}

func runCapabilityCommand(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	return cmd.Output()
}

func (c *RunnerConfig) discoverCapabilities(ctx context.Context, command capabilityCommand, icmpSupport func() (bool, bool)) urth.WorkerCapabilities {
	version := prob.BuildVersion()
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = time.Minute
	}
	caps := urth.WorkerCapabilities{
		Version: version, OS: runtime.GOOS, Architecture: runtime.GOARCH,
		ProbeVersions: map[string]string{}, RuntimeVersions: map[string]string{},
		MinDuration: time.Nanosecond.String(), MaxDuration: timeout.String(),
	}
	for _, tool := range []struct{ name, arg, key, prefix string }{
		{"node", "--version", "node", "v"},
		{"python3", "--version", "python", "Python "},
		{"npm", "--version", "npm", ""},
	} {
		if out, err := command(ctx, "", tool.name, tool.arg); err == nil {
			value := strings.TrimPrefix(strings.TrimSpace(string(out)), tool.prefix)
			if value != "" {
				caps.RuntimeVersions[tool.key] = value
			}
		}
	}
	icmpAvailable, rawSockets := icmpSupport()
	if rawSockets {
		caps.Privileges = []string{"raw-sockets"}
	}
	for kind, registration := range prob.ListProbs() {
		switch kind {
		case "icmp":
			if !icmpAvailable {
				continue
			}
		case "puppeteer":
			if !browserProfile || caps.RuntimeVersions["node"] == "" || caps.RuntimeVersions["npm"] == "" {
				continue
			}
			// The prober runs from this directory. Discover its installed packages and
			// browser here, without downloading or installing anything during enrollment.
			out, err := command(ctx, c.WorkingDirectory, "node", "-e", browserCapabilityScript)
			if err != nil {
				continue
			}
			var browser struct {
				Puppeteer string `json:"puppeteer"`
				HAR       string `json:"har"`
				Path      string `json:"path"`
			}
			if json.Unmarshal(out, &browser) != nil || browser.Puppeteer == "" || browser.HAR == "" || browser.Path == "" {
				continue
			}
			caps.RuntimeVersions["puppeteer"] = browser.Puppeteer
			caps.RuntimeVersions["puppeteer-har"] = browser.HAR
			out, err = command(ctx, c.WorkingDirectory, browser.Path, "--version")
			if err != nil || strings.TrimSpace(string(out)) == "" {
				continue
			}
			caps.RuntimeVersions["browser"] = browserVersion(string(out))
		case "pypuppeteer":
			// This runtime is not part of either supported Worker profile.
			continue
		}
		caps.ProbeVersions[string(kind)] = registration.Version
	}
	return caps
}

const browserCapabilityScript = `const fs = require('fs'); const p = require('puppeteer'); require('puppeteer-har'); const path = p.executablePath(); fs.accessSync(path, fs.constants.X_OK); console.log(JSON.stringify({puppeteer: require('puppeteer/package.json').version, har: require('puppeteer-har/package.json').version, path}));`

// Browser executables use product prefixes and often four-component versions.
// Keep the exact numeric token. Do not truncate it into a semantic version.
func browserVersion(output string) string {
	for _, field := range strings.Fields(output) {
		if len(field) > 0 && field[0] >= '0' && field[0] <= '9' && strings.Contains(field, ".") {
			return field
		}
	}
	return strings.TrimSpace(output)
}

// Raw sockets are tested rather than inferred from UID or an operator label.
// Linux can also allow unprivileged ICMP datagram sockets through ping_group_range.
func discoverICMP() (available, raw bool) {
	for _, network := range []string{"ip4:icmp", "ip6:ipv6-icmp"} {
		if socket, err := icmp.ListenPacket(network, ""); err == nil {
			_ = socket.Close()
			return true, true
		}
	}
	for _, network := range []string{"udp4", "udp6"} {
		if socket, err := icmp.ListenPacket(network, ""); err == nil {
			_ = socket.Close()
			return true, false
		}
	}
	return false, false
}
