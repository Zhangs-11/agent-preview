package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Zhangs-11/agent-preview/internal/preview"
	"github.com/Zhangs-11/agent-preview/internal/tunnel"
)

var Version = "0.1.0-dev"

var supportedFileExtensions = map[string]bool{
	".gif": true, ".htm": true, ".html": true, ".jpeg": true, ".jpg": true,
	".pdf": true, ".png": true, ".svg": true, ".txt": true, ".webp": true,
}

func Run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stdout)
		return nil
	}

	switch args[0] {
	case "publish":
		return runPublish(args[1:], stdout, stderr)
	case "list":
		return runList(args[1:], stdout)
	case "stop":
		return runStop(args[1:], stdout)
	case "setup":
		return runSetup(args[1:], stdout)
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, Version)
		return nil
	case "__worker":
		return runWorker(args[1:])
	case "help", "--help", "-h":
		printUsage(stdout)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runPublish(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("publish", flag.ContinueOnError)
	flags.SetOutput(stderr)
	ttlText := flags.String("ttl", "24h", "preview lifetime; use 0 to keep it until stopped")
	port := flags.Int("port", 0, "proxy an HTTP service on 127.0.0.1:<port>")
	jsonOutput := flags.Bool("json", false, "print machine-readable JSON")
	localOnly := flags.Bool("local-only", false, "skip the public tunnel and return a localhost URL")
	if err := flags.Parse(args); err != nil {
		return err
	}

	ttl, err := parseTTL(*ttlText)
	if err != nil {
		return err
	}
	request, err := buildRequest(flags.Args(), *port, ttl, *localOnly)
	if err != nil {
		return err
	}
	if err := startWorker(request); err != nil {
		return err
	}

	state, err := waitForReady(request.ID, 240*time.Second)
	if err != nil {
		_ = requestStop(request.ID)
		return err
	}
	return printState(stdout, state, *jsonOutput)
}

func parseTTL(value string) (time.Duration, error) {
	if value == "0" {
		return 0, nil
	}
	ttl, err := time.ParseDuration(value)
	if err != nil || ttl < time.Second {
		return 0, fmt.Errorf("invalid --ttl %q; use a duration such as 24h, 72h, or 0", value)
	}
	return ttl, nil
}

func buildRequest(arguments []string, port int, ttl time.Duration, localOnly bool) (workerRequest, error) {
	id, err := randomHex(8)
	if err != nil {
		return workerRequest{}, err
	}
	token, err := randomHex(32)
	if err != nil {
		return workerRequest{}, err
	}

	request := workerRequest{
		ID: id, Token: token, TTLSeconds: int64(ttl.Seconds()), LocalOnly: localOnly, CreatedAt: time.Now().UTC(),
	}
	if port != 0 {
		if len(arguments) != 0 {
			return workerRequest{}, errors.New("publish accepts either a path or --port, not both")
		}
		if port < 1 || port > 65535 {
			return workerRequest{}, errors.New("--port must be between 1 and 65535")
		}
		request.Kind = preview.KindProxy
		request.Target = "http://127.0.0.1:" + strconv.Itoa(port)
		return request, nil
	}

	if len(arguments) != 1 {
		return workerRequest{}, errors.New("publish requires one file or directory path")
	}
	target, err := filepath.Abs(arguments[0])
	if err != nil {
		return workerRequest{}, fmt.Errorf("resolve preview target: %w", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		return workerRequest{}, fmt.Errorf("inspect preview target: %w", err)
	}
	request.Target = target
	if info.IsDir() {
		if !hasSafeIndexFile(target) {
			return workerRequest{}, errors.New("preview directory must contain index.html or index.htm")
		}
		request.Kind = preview.KindDirectory
		return request, nil
	}
	if !info.Mode().IsRegular() {
		return workerRequest{}, errors.New("preview target must be a regular file or directory")
	}
	extension := strings.ToLower(filepath.Ext(target))
	if !supportedFileExtensions[extension] {
		return workerRequest{}, fmt.Errorf("unsupported preview file type %q", extension)
	}
	request.Kind = preview.KindFile
	return request, nil
}

func hasSafeIndexFile(directory string) bool {
	realRoot, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return false
	}
	for _, name := range []string{"index.html", "index.htm"} {
		indexPath, err := filepath.EvalSymlinks(filepath.Join(realRoot, name))
		if err != nil {
			continue
		}
		relative, err := filepath.Rel(realRoot, indexPath)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		if info, err := os.Stat(indexPath); err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

func randomHex(byteCount int) (string, error) {
	value := make([]byte, byteCount)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate random value: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func startWorker(request workerRequest) error {
	requestFile, err := requestPath(request.ID)
	if err != nil {
		return err
	}
	if err := writeJSON(requestFile, request); err != nil {
		return fmt.Errorf("write preview request: %w", err)
	}
	startingState := State{
		ID: request.ID, Status: "starting", Kind: request.Kind, Target: request.Target,
		Visibility: visibility(request.LocalOnly), CreatedAt: request.CreatedAt,
	}
	if request.TTLSeconds > 0 {
		expiresAt := request.CreatedAt.Add(time.Duration(request.TTLSeconds) * time.Second)
		startingState.ExpiresAt = &expiresAt
	}
	if err := writeState(startingState); err != nil {
		return fmt.Errorf("write initial preview state: %w", err)
	}

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate agent-preview executable: %w", err)
	}
	cmd := exec.Command(executable, "__worker", "--request", requestFile)
	directory, err := previewDirectory(request.ID)
	if err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(directory, "worker.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open worker log: %w", err)
	}
	defer logFile.Close()
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	configureDetached(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start preview worker: %w", err)
	}
	if err := cmd.Process.Release(); err != nil {
		_ = cmd.Process.Kill()
		return fmt.Errorf("release preview worker: %w", err)
	}
	return nil
}

func waitForReady(id string, timeout time.Duration) (State, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		state, err := readState(id)
		if err == nil {
			switch state.Status {
			case "ready":
				return state, nil
			case "error":
				return State{}, errors.New(state.Error)
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return State{}, errors.New("timed out waiting for the preview worker")
}

func printState(writer io.Writer, state State, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(writer).Encode(state)
	}
	fmt.Fprintf(writer, "Preview ready (%s)\n%s\nID: %s\n", state.Visibility, state.URL, state.ID)
	if state.Provider != "" {
		fmt.Fprintf(writer, "Provider: %s\n", state.Provider)
	}
	if state.ExpiresAt == nil {
		fmt.Fprintln(writer, "Expires: when stopped")
	} else {
		fmt.Fprintf(writer, "Expires: %s\n", state.ExpiresAt.Local().Format(time.RFC3339))
	}
	return nil
}

func runList(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("list", flag.ContinueOnError)
	jsonOutput := flags.Bool("json", false, "print machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("list does not accept positional arguments")
	}
	states, err := listStates()
	if err != nil {
		return err
	}
	if *jsonOutput {
		return json.NewEncoder(stdout).Encode(states)
	}
	if len(states) == 0 {
		fmt.Fprintln(stdout, "No previews found.")
		return nil
	}
	for _, state := range states {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", state.ID, state.Status, state.Visibility, state.Target)
	}
	return nil
}

func runStop(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return errors.New("stop requires one preview ID")
	}
	if err := requestStop(args[0]); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Stop requested for preview %s.\n", args[0])
	return nil
}

func runSetup(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("setup", flag.ContinueOnError)
	accepted := flags.Bool("accept-cloudflare-terms", false, "confirm acceptance of Cloudflare's license, terms, and privacy policy")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("setup does not accept positional arguments")
	}
	path, version, err := tunnel.Install(context.Background(), *accepted)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "cloudflared %s is ready at %s\n", version, path)
	return nil
}

func visibility(localOnly bool) string {
	if localOnly {
		return "local"
	}
	return "public"
}

func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, `agent-preview creates temporary browser links for agent-built files.

Usage:
  agent-preview setup --accept-cloudflare-terms
  agent-preview publish [--ttl 24h] [--json] <file-or-directory>
  agent-preview publish [--ttl 24h] [--json] --port <port>
  agent-preview list [--json]
  agent-preview stop <preview-id>
  agent-preview version`)
}

func checkProxyTarget(target string) error {
	connection, err := net.DialTimeout("tcp", strings.TrimPrefix(target, "http://"), 2*time.Second)
	if err != nil {
		return fmt.Errorf("proxy target is not reachable: %w", err)
	}
	return connection.Close()
}
