package puppeteer

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"strings"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

const (
	Kind           = prob.Kind("puppeteer")
	ScriptMimeType = "text/javascript"
)

var nodeSystemFiles = []string{
	"package.json",
	"node_modules",
}

type Spec struct {
	Port   int    `json:"port,omitempty" yaml:"port,omitempty"`
	Script string `json:"script,omitempty" yaml:"script,omitempty"`
}

func init() {
	moduleVersion := "devel"
	if bi, ok := debug.ReadBuildInfo(); ok {
		moduleVersion = strings.Trim(bi.Main.Version, "()")
	}

	// Ignore double registration error
	_ = prob.RegisterProbKind(
		Kind,
		&Spec{},
		prob.ProbRegistration{
			RunFunc:     RunScript,
			ContentType: ScriptMimeType,
			Version:     moduleVersion,
		})
}

func setupNodeDir(dir string) error {
	cmd := exec.Command("npm", "init", "-y")
	cmd.Dir = dir

	return cmd.Run()
}

func installPuppeteer(dir string) error {
	cmd := exec.Command("npm", "install", "puppeteer", "puppeteer-har")
	cmd.Dir = dir

	return cmd.Run()
}

func SetupRunEnv(workDir string, logger log.Logger) error {
	if _, err := os.Stat(path.Join(workDir, "package.json")); err == nil {
		return nil
	}

	logger.Log("Creating node working directory", "dir", workDir)
	if err := setupNodeDir(workDir); err != nil {
		logger.Log("Failed to create working directory", "dir", workDir, "err", err)
		return err
	}

	logger.Log("Installing Puppeteer and dependencies", "dir", workDir)
	if err := installPuppeteer(workDir); err != nil {
		logger.Log("Failed to install Puppeteer and dependencies", "dir", workDir, "err", err)
		return err
	}

	return nil
}

func RunScript(ctx context.Context, probSpec any, config prob.RunOptions, registry *prometheus.Registry, logger log.Logger) (prob.RunStatus, []prob.Artifact, error) {
	spec, ok := probSpec.(*Spec)
	if !ok {
		return prob.RunFinishedError, nil, fmt.Errorf("%w: got %q, expected %q", manifest.ErrUnexpectedSpecType, reflect.TypeOf(probSpec), reflect.TypeOf(&Spec{}))
	}
	logger.Log("Running puppeteer script")

	// TODO: Check that working directory exists and writable!
	if err := SetupRunEnv(config.Puppeteer.WorkingDirectory, logger); err != nil {
		logger.Log("Failed to setup work environment", "err", err)

		return prob.RunFinishedError, nil, fmt.Errorf("failed to initialize work directory: %w", err)
	}

	workDir, err := os.MkdirTemp(config.Puppeteer.WorkingDirectory, config.Puppeteer.TempDirPrefix)
	if err != nil {
		logger.Log("Failed to create work directory", "err", err)

		return prob.RunFinishedError, nil, fmt.Errorf("failed to create work directory: %w", err)
	}

	defer func(dir string, keep bool) {
		if !keep {
			os.RemoveAll(dir)
		}
	}(workDir, config.Puppeteer.KeepTempDir)
	logger.Log("Working directory configured", "dir", workDir, "keep", config.Puppeteer.KeepTempDir)

	cmd := exec.CommandContext(ctx, "node", "-")
	// cmd.Env = append(cmd.Env, fmt.Sprintf("PUPPETEER_CACHE_DIR=%v", options.Puppeteer.WorkingDirectory))

	// FIXME: Breaks on latest version of puppeteer
	// hasDisplay := os.Getenv("DISPLAY")
	// if hasDisplay != "" {
	// 	cmd.Env = append(cmd.Env, fmt.Sprintf("DISPLAY=%v", hasDisplay))
	// }

	cmd.Env = append(cmd.Env, fmt.Sprintf("URTH_PUPPETEER_HEADLESS=%t", config.Puppeteer.Headless))
	if config.Puppeteer.PageWaitSeconds != 0 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("URTH_PUPPETEER_PAGE_WAIT=%d", config.Puppeteer.PageWaitSeconds))
	}

	cmd.Dir = workDir

	inPipe, err := cmd.StdinPipe()
	if err != nil {
		err := fmt.Errorf("failed to open input pipe: %w", err)
		logger.Log(err)
		return prob.RunFinishedError, nil, nil
	}

	// TODO: Write common prolog for all scrips
	go func() {
		defer inPipe.Close()
		n, err := inPipe.Write([]byte(spec.Script))
		if err != nil {
			logger.Log("failed to write script into the nodejs input pipe: ", err)
		}
		logger.Log("Script loaded", "bytes", n)
	}()

	// TODO: Capture artifacts and store HAR file
	runResult := prob.RunFinishedSuccess

	cmd.Stderr = log.NewStdlibAdapter(logger)
	cmd.Stdout = log.NewStdlibAdapter(logger)

	// Run the proces
	if err := cmd.Run(); err != nil {
		logger.Log("Failed to execute command", "err", err)
		runResult = prob.RunFinishedError
	}

	// Capture artifacts:
	artifacts := make([]prob.Artifact, 0)
	workDirEntries, err := os.ReadDir(workDir)
	if err != nil {
		logger.Log("Failed to open working directory (No artifacts can be captured)", "err", err)
	} else {
		for _, entry := range workDirEntries {
			if entry.Name() == "node_modules" || entry.Name() == "package.json" {
				continue
			}

			if entry.IsDir() {
				logger.Log("skipping artifact directory ", entry.Name())
				continue
			}

			data, err := os.ReadFile(filepath.Join(workDir, entry.Name()))
			if err != nil {
				logger.Log("failed to capture artifact ", entry.Name(), ": ", err)
				continue
			}

			artifacts = append(artifacts, prob.Artifact{
				Rel:      filepath.Ext(entry.Name()),
				MimeType: http.DetectContentType(data),
				Content:  data,
			})
		}
	}

	return runResult, artifacts, nil
}
