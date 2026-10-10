package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Zhangs-11/agent-preview/internal/preview"
	"github.com/Zhangs-11/agent-preview/internal/tunnel"
)

// 公网地址可能在隧道进程仍存活时失效（如 localhost.run 免费地址轮换），需要持续从公网检查
var (
	tunnelProbeInterval    = time.Minute
	tunnelProbeMaxFailures = 3
)

func runWorker(args []string) error {
	flags := flag.NewFlagSet("__worker", flag.ContinueOnError)
	requestFile := flags.String("request", "", "worker request path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *requestFile == "" {
		return errors.New("worker request path is required")
	}

	var request workerRequest
	if err := readJSON(*requestFile, &request); err != nil {
		return fmt.Errorf("read worker request: %w", err)
	}
	state := State{
		ID: request.ID, Status: "starting", Kind: request.Kind, Target: request.Target,
		Visibility: visibility(request.LocalOnly), CreatedAt: request.CreatedAt,
	}
	if request.TTLSeconds > 0 {
		expiresAt := request.CreatedAt.Add(time.Duration(request.TTLSeconds) * time.Second)
		state.ExpiresAt = &expiresAt
	}
	if err := writeState(state); err != nil {
		return err
	}

	if request.Kind == preview.KindProxy {
		if err := checkProxyTarget(request.Target); err != nil {
			return failWorker(state, err)
		}
	}
	handler, err := preview.NewHandler(preview.Config{
		Kind: request.Kind, Target: request.Target, Token: request.Token, SecureCookie: !request.LocalOnly,
	})
	if err != nil {
		return failWorker(state, err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return failWorker(state, fmt.Errorf("listen for preview: %w", err))
	}
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	serverError := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if !errors.Is(err, http.ErrServerClosed) {
			serverError <- err
		}
		close(serverError)
	}()

	localBaseURL := "http://" + listener.Addr().String()
	state.LocalURL = localBaseURL
	var tunnelProcess *tunnel.Process
	var tunnelLock *providerLock
	var tunnelDone <-chan error
	var tunnelUnreachable <-chan error
	monitorContext, stopMonitor := context.WithCancel(context.Background())
	defer stopMonitor()
	if request.LocalOnly {
		state.URL = localBaseURL + "/?token=" + request.Token
		state.Provider = "local"
	} else {
		tunnelProcess, tunnelLock, err = startReachableTunnel(context.Background(), localBaseURL)
		if err != nil {
			_ = server.Close()
			return failWorker(state, err)
		}
		state.URL = tunnelProcess.URL + "/?token=" + request.Token
		state.Provider = tunnelProcess.Provider
		tunnelDone = tunnelProcess.Done()
		tunnelUnreachable = monitorTunnel(monitorContext, tunnelProcess.URL, tunnelProbeInterval, tunnelProbeMaxFailures)
	}
	state.Status = "ready"
	if err := writeState(state); err != nil {
		if tunnelProcess != nil {
			if tunnelProcess.StopAndWait(2 * time.Second) {
				tunnelLock.release()
			} else {
				tunnelLock.closeWithoutUnlock()
			}
		} else {
			tunnelLock.release()
		}
		_ = server.Close()
		return err
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	var expiration <-chan time.Time
	var expirationTimer *time.Timer
	if state.ExpiresAt != nil {
		remaining := time.Until(*state.ExpiresAt)
		if remaining < 0 {
			remaining = 0
		}
		expirationTimer = time.NewTimer(remaining)
		expiration = expirationTimer.C
		defer expirationTimer.Stop()
	}
	stopTicker := time.NewTicker(250 * time.Millisecond)
	defer stopTicker.Stop()

	finalStatus := "stopped"
	var finalError error
	stopFile, err := stopPath(request.ID)
	if err != nil {
		return failWorker(state, err)
	}

running:
	for {
		select {
		case <-expiration:
			finalStatus = "expired"
			break running
		case <-signals:
			break running
		case <-stopTicker.C:
			if _, err := os.Stat(stopFile); err == nil {
				break running
			} else if !errors.Is(err, os.ErrNotExist) {
				finalStatus = "error"
				finalError = err
				break running
			}
		case err := <-serverError:
			if err != nil {
				finalStatus = "error"
				finalError = fmt.Errorf("preview server stopped: %w", err)
			}
			break running
		case err := <-tunnelDone:
			if err != nil {
				finalStatus = "error"
				finalError = fmt.Errorf("%s tunnel stopped: %w", state.Provider, err)
			}
			break running
		case err := <-tunnelUnreachable:
			finalStatus = "error"
			finalError = fmt.Errorf("%s public URL is no longer reachable: %w", state.Provider, err)
			break running
		}
	}

	if tunnelProcess != nil {
		if tunnelProcess.StopAndWait(3 * time.Second) {
			tunnelLock.release()
		} else {
			tunnelLock.closeWithoutUnlock()
		}
	} else {
		tunnelLock.release()
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownContext)
	state.Status = finalStatus
	if finalError != nil {
		state.Error = finalError.Error()
	}
	return writeState(state)
}

func startReachableTunnel(ctx context.Context, localURL string) (*tunnel.Process, *providerLock, error) {
	var failures []error
	if binary, err := tunnel.FindBinary(); err == nil {
		process, startErr := tunnel.StartCloudflare(ctx, binary, localURL, 30*time.Second)
		if startErr == nil {
			// Quick Tunnel 域名在打印后约 1 秒才生效，过早查询的 NXDOMAIN 会被本机 DNS 缓存 60 秒，
			// 所以先等待再查，并让重试覆盖完整的否定缓存时间
			time.Sleep(3 * time.Second)
			if probeErr := probeTunnel(process.URL, 14, 5*time.Second, 5*time.Second); probeErr == nil {
				return process, nil, nil
			} else {
				failures = append(failures, fmt.Errorf("cloudflare tunnel is not reachable from this network: %w", probeErr))
				process.StopAndWait(2 * time.Second)
			}
		} else {
			failures = append(failures, fmt.Errorf("cloudflare tunnel failed: %w", startErr))
		}
	} else {
		failures = append(failures, err)
	}

	ssh, err := tunnel.FindSSH()
	if err != nil {
		failures = append(failures, err)
		return nil, nil, fmt.Errorf("no public preview provider succeeded: %w", errors.Join(failures...))
	}
	lock, err := acquireLocalhostRunLock()
	if err != nil {
		failures = append(failures, err)
		return nil, nil, fmt.Errorf("no public preview provider succeeded: %w", errors.Join(failures...))
	}
	process, err := tunnel.StartLocalhostRun(ctx, ssh, localURL, lock.inheritedFiles(), 30*time.Second)
	if err != nil {
		lock.release()
		failures = append(failures, fmt.Errorf("localhost.run tunnel failed: %w", err))
		return nil, nil, fmt.Errorf("no public preview provider succeeded: %w", errors.Join(failures...))
	}
	if err := probeTunnel(process.URL, 3, time.Second, 5*time.Second); err != nil {
		if process.StopAndWait(2 * time.Second) {
			lock.release()
		} else {
			lock.closeWithoutUnlock()
		}
		failures = append(failures, fmt.Errorf("localhost.run tunnel is not reachable: %w", err))
		return nil, nil, fmt.Errorf("no public preview provider succeeded: %w", errors.Join(failures...))
	}
	return process, lock, nil
}

// monitorTunnel 按间隔检查公网健康地址，连续失败达到上限时返回最后一次错误
func monitorTunnel(ctx context.Context, baseURL string, interval time.Duration, maxFailures int) <-chan error {
	result := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		failures := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				err := probeTunnel(baseURL, 1, 0, 10*time.Second)
				if err == nil {
					failures = 0
					continue
				}
				failures++
				if failures >= maxFailures {
					result <- err
					return
				}
			}
		}
	}()
	return result
}

func probeTunnel(baseURL string, attempts int, retryDelay, timeout time.Duration) error {
	client := &http.Client{Timeout: timeout}
	var lastError error
	for attempt := 0; attempt < attempts; attempt++ {
		response, err := client.Get(baseURL + preview.HealthPath)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			_ = response.Body.Close()
			if response.StatusCode == http.StatusNoContent {
				return nil
			}
			err = fmt.Errorf("health check returned HTTP %d", response.StatusCode)
		}
		lastError = err
		if attempt+1 < attempts {
			time.Sleep(retryDelay)
		}
	}
	return lastError
}

func failWorker(state State, err error) error {
	state.Status = "error"
	state.Error = err.Error()
	if writeErr := writeState(state); writeErr != nil {
		return fmt.Errorf("%v; additionally failed to write worker state: %w", err, writeErr)
	}
	return err
}
