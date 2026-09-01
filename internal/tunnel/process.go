package tunnel

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Process struct {
	URL      string
	Provider string
	cancel   context.CancelFunc
	done     <-chan error
}

type recentOutput struct {
	mu    sync.Mutex
	lines []string
}

func (output *recentOutput) add(line string) {
	output.mu.Lock()
	defer output.mu.Unlock()
	if len(line) > 2048 {
		line = line[:2048] + "..."
	}
	output.lines = append(output.lines, line)
	if len(output.lines) > 20 {
		output.lines = output.lines[len(output.lines)-20:]
	}
}

func (output *recentOutput) text() string {
	output.mu.Lock()
	defer output.mu.Unlock()
	return strings.Join(output.lines, "\n")
}

func startProcess(parent context.Context, binary string, arguments []string, extraFiles []*os.File, pattern *regexp.Regexp, provider string, timeout time.Duration) (*Process, error) {
	ctx, cancel := context.WithCancel(parent)
	cmd := exec.CommandContext(ctx, binary, arguments...)
	cmd.ExtraFiles = extraFiles

	reader, writer := io.Pipe()
	cmd.Stdout = writer
	cmd.Stderr = writer
	if err := cmd.Start(); err != nil {
		cancel()
		_ = reader.Close()
		_ = writer.Close()
		return nil, fmt.Errorf("start %s tunnel: %w", provider, err)
	}

	urlChannel := make(chan string, 1)
	var once sync.Once
	var output recentOutput
	go func() {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := scanner.Text()
			output.add(line)
			if match := pattern.FindString(line); match != "" {
				once.Do(func() { urlChannel <- match })
			}
		}
	}()

	done := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		_ = writer.Close()
		done <- err
		close(done)
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case publicURL := <-urlChannel:
		return &Process{URL: publicURL, Provider: provider, cancel: cancel, done: done}, nil
	case err := <-done:
		cancel()
		if err == nil {
			err = errors.New("tunnel process exited before returning a URL")
		}
		return nil, withRecentOutput(err, output.text())
	case <-timer.C:
		cancel()
		return nil, withRecentOutput(fmt.Errorf("timed out waiting for a %s tunnel URL", provider), output.text())
	}
}

func (process *Process) Done() <-chan error {
	return process.done
}

func (process *Process) Stop() {
	process.cancel()
}

func (process *Process) StopAndWait(timeout time.Duration) bool {
	process.Stop()
	select {
	case <-process.Done():
		return true
	case <-time.After(timeout):
		return false
	}
}

func withRecentOutput(err error, output string) error {
	if strings.TrimSpace(output) == "" {
		return err
	}
	return fmt.Errorf("%w; recent tunnel output:\n%s", err, output)
}
