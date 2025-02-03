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

	"github.com/sre-norns/urth/pkg/runner"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

const (
	Kind           = urth.ProbKind("puppeteer")
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
	moduleVersion := "unknown"
	if bi, ok := debug.ReadBuildInfo(); ok {
		moduleVersion = strings.Trim(bi.Main.Version, "()")
	}

	// Ignore double registration error
	_ = runner.RegisterProbKind(
		Kind,
		&Spec{},
		runner.ProbRegistration{
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

func SetupRunEnv(workDir string, logger *runner.RunLog) error {
	if _, err := os.Stat(path.Join(workDir, "package.json")); err == nil {
		return nil
	}

	logger.Logf("Creating node working directory at %q", workDir)
	if err := setupNodeDir(workDir); err != nil {
		return err
	}

	logger.Logf("installing Puppeteer and dependecies %q", workDir)
	if err := installPuppeteer(workDir); err != nil {
		return err
	}

	return nil
}

func RunScript(ctx context.Context, probSpec any, logger *runner.RunLog, options runner.RunOptions) (urth.ResultStatus, []urth.ArtifactSpec, error) {
	prob, ok := probSpec.(*Spec)
	if !ok {
		return urth.NewRunResults(urth.RunFinishedError), logger.Package(), fmt.Errorf("%w: got %q, expected %q", manifest.ErrUnexpectedSpecType, reflect.TypeOf(probSpec), reflect.TypeOf(&Spec{}))
	}
	logger.Log("Running puppeteer script")

	// TODO: Check that working directory exists and writable!
	if err := SetupRunEnv(options.Puppeteer.WorkingDirectory, logger); err != nil {
		err = fmt.Errorf("failed to initialize work directory: %w", err)
		logger.Log(err)

		return urth.NewRunResults(urth.RunFinishedError), logger.Package(), nil
	}

	workDir, err := os.MkdirTemp(options.Puppeteer.WorkingDirectory, options.Puppeteer.TempDirPrefix)
	if err != nil {
		err = fmt.Errorf("failed to create work directory: %w", err)
		logger.Log(err)
		return urth.NewRunResults(urth.RunFinishedError), logger.Package(), nil
	}

	defer func(dir string, keep bool) {
		if !keep {
			os.RemoveAll(dir)
		}
	}(workDir, options.Puppeteer.KeepTempDir)
	logger.Logf("working directory: %q (will be kept?: %t)", workDir, options.Puppeteer.KeepTempDir)

	cmd := exec.CommandContext(ctx, "node", "-")
	// cmd.Env = append(cmd.Env, fmt.Sprintf("PUPPETEER_CACHE_DIR=%v", options.Puppeteer.WorkingDirectory))

	// FIXME: Breaks on latest version of puppeteer
	// hasDisplay := os.Getenv("DISPLAY")
	// if hasDisplay != "" {
	// 	cmd.Env = append(cmd.Env, fmt.Sprintf("DISPLAY=%v", hasDisplay))
	// }

	cmd.Env = append(cmd.Env, fmt.Sprintf("URTH_PUPPETEER_HEADLESS=%t", options.Puppeteer.Headless))
	if options.Puppeteer.PageWaitSeconds != 0 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("URTH_PUPPETEER_PAGE_WAIT=%d", options.Puppeteer.PageWaitSeconds))
	}

	cmd.Dir = workDir

	inPipe, err := cmd.StdinPipe()
	if err != nil {
		err := fmt.Errorf("failed to open input pipe: %w", err)
		logger.Log(err)
		return urth.NewRunResults(urth.RunFinishedError), logger.Package(), nil
	}

	// TODO: Write common prolog for all scrips
	go func() {
		defer inPipe.Close()
		n, err := inPipe.Write([]byte(prob.Script))
		if err != nil {
			logger.Log("failed to write script into the nodejs input pipe: ", err)
		}
		logger.Logf("script loaded: %d bytes", n)
	}()

	// TODO: Capture artifacts and store HAR file
	runResult := urth.RunFinishedSuccess

	cmd.Stderr = logger
	cmd.Stdout = logger

	// Run the proces
	if err := cmd.Run(); err != nil {
		logger.Log("failed to execture cmd: ", err)
		runResult = urth.RunFinishedError
	}

	// Capture artifacts:
	artifacts := make([]urth.ArtifactSpec, 0)
	workDirEntries, err := os.ReadDir(workDir)
	if err != nil {
		logger.Log("Failed to open working directory. No artifacts will be captured: ", err)
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

			artifacts = append(artifacts, urth.ArtifactSpec{
				Rel:      filepath.Ext(entry.Name()),
				MimeType: http.DetectContentType(data),
				Content:  data,
			})
		}
	}

	return urth.NewRunResults(runResult), append(artifacts, logger.ToArtifact()), nil
}
