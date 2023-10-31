package runner

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"

	"github.com/sre-norns/urth/pkg/urth"
)

func setupNodeDir(dir string) error {
	cmd := exec.Command("npm", "init", "-y")
	cmd.Dir = dir

	return cmd.Run()
}

func installPuppeteer(dir string) error {
	cmd := exec.Command("npm", "install", "puppeteer")
	cmd.Dir = dir

	return cmd.Run()
}

func SetupRunEnv(workDir string) error {
	if _, err := os.Stat(path.Join(workDir, "package.json")); err == nil {
		return nil
	}

	if err := setupNodeDir(workDir); err != nil {
		return err
	}

	if err := installPuppeteer(workDir); err != nil {
		return err
	}

	return nil
}

func runPuppeteerScript(ctx context.Context, scriptContent []byte, options RunOptions) (urth.FinalRunResults, error) {
	log.Println("running puppeteer...")

	workDir, err := os.MkdirTemp(options.Puppeteer.WorkingDirectory, options.Puppeteer.TempDirPrefix)
	if err != nil {
		return urth.NewRunResults(urth.RunFinishedError), fmt.Errorf("failed create work directory: %w", err)
	}

	defer func(dir string, keep bool) {
		if !keep {
			os.RemoveAll(dir)
		}
	}(workDir, options.Puppeteer.KeepTempDir)
	log.Printf("working directory: %q (will be kept: %t)", workDir, options.Puppeteer.KeepTempDir)

	if err := SetupRunEnv(options.Puppeteer.WorkingDirectory); err != nil {
		return urth.NewRunResults(urth.RunFinishedError), err
	}

	cmd := exec.Command("node", "-")
	cmd.Env = append(cmd.Env, fmt.Sprintf("PUPPETEER_HEADLESS=%t", options.Puppeteer.Headless))
	cmd.Env = append(cmd.Env, fmt.Sprintf("PUPPETEER_PAGE_WAIT=%d", options.Puppeteer.PageWaitSeconds))
	cmd.Dir = workDir

	inPipe, err := cmd.StdinPipe()
	if err != nil {
		return urth.NewRunResults(urth.RunFinishedError), fmt.Errorf("failed to open input pipe: %w", err)
	}

	// TODO: Write common prolog for all scrips
	go func() {
		defer inPipe.Close()
		n, err := inPipe.Write(scriptContent)
		if err != nil {
			fmt.Printf("failed to write script into input pipe: %v\n", err)
		}
		log.Printf("script loaded: %d bytes...\n", n)
	}()

	out, err := cmd.CombinedOutput()
	// TODO: Store logs as a build artifact and ship url to the server
	// TODO: Capture and store HAR file
	fmt.Println(string(out))

	runResult := urth.RunFinishedSuccess
	if err != nil {
		runResult = urth.RunFinishedError
	}

	return urth.NewRunResults(runResult), err
}
