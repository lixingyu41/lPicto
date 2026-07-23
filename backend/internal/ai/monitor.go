package ai

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"lpicto/backend/internal/db"
)

const AIHealthCheckInterval = 30 * time.Minute

func StartHealthMonitor(ctx context.Context, database *db.DB, baseURL string, logger *slog.Logger) {
	go func() {
		runAIHealthCheck(ctx, database, baseURL, logger)
		ticker := time.NewTicker(AIHealthCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runAIHealthCheck(ctx, database, baseURL, logger)
			}
		}
	}()
}

// RunHealthCheck executes the same check used by the scheduled monitor.
func RunHealthCheck(ctx context.Context, database *db.DB, baseURL string, logger *slog.Logger) {
	runAIHealthCheck(ctx, database, baseURL, logger)
}

// RestartService requests an AI worker restart and verifies recovery.
func RestartService(ctx context.Context, baseURL string) error {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if err := requestAIRestart(ctx, baseURL); err != nil {
		return err
	}
	if !waitForAIHealth(ctx, baseURL, 3*time.Minute) {
		return fmt.Errorf("AI 服务重启后仍未恢复")
	}
	return nil
}

func runAIHealthCheck(ctx context.Context, database *db.DB, baseURL string, logger *slog.Logger) {
	if err := database.BeginSystemTask(ctx, db.SystemTaskAIHealth); err != nil {
		logger.Warn("start AI health task failed", "error", err)
		return
	}
	enabled, err := database.AIExecutionEnabled(ctx)
	if err != nil {
		finishAIHealthTask(ctx, database, "failed", "读取 AI 运行设置失败", logger)
		return
	}
	if !enabled {
		finishAIHealthTask(ctx, database, "skipped", "自动分析和手动全库分析均未启用", logger)
		return
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if waitForAIHealth(ctx, baseURL, time.Minute) {
		finishAIHealthTask(ctx, database, "success", "AI 服务运行正常", logger)
		return
	}
	restartErr := requestAIRestart(ctx, baseURL)
	if waitForAIHealth(ctx, baseURL, 3*time.Minute) {
		finishAIHealthTask(ctx, database, "success", "检测到异常，AI 服务已自动重启并恢复", logger)
		return
	}
	message := "AI 服务异常，自动重启后仍未恢复"
	if restartErr != nil {
		message = fmt.Sprintf("AI 服务异常，重启请求失败：%v", restartErr)
	}
	finishAIHealthTask(ctx, database, "failed", message, logger)
}

func finishAIHealthTask(ctx context.Context, database *db.DB, status string, message string, logger *slog.Logger) {
	if ctx.Err() != nil {
		return
	}
	if err := database.FinishSystemTask(ctx, db.SystemTaskAIHealth, status, message); err != nil {
		logger.Warn("finish AI health task failed", "status", status, "error", err)
	}
}

func waitForAIHealth(ctx context.Context, baseURL string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if probeAIHealth(ctx, baseURL) {
			return true
		}
		if time.Now().Add(5 * time.Second).After(deadline) {
			return false
		}
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}

func probeAIHealth(ctx context.Context, baseURL string) bool {
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, baseURL+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func requestAIRestart(ctx context.Context, baseURL string) error {
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, baseURL+"/restart", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
