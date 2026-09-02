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

func StartHealthMonitor(ctx context.Context, database *db.DB, baseURL, externalToken string, logger *slog.Logger) {
	go func() {
		runAIHealthCheck(ctx, database, baseURL, externalToken, logger)
		ticker := time.NewTicker(AIHealthCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runAIHealthCheck(ctx, database, baseURL, externalToken, logger)
			}
		}
	}()
}

// RunHealthCheck executes the same check used by the scheduled monitor.
func RunHealthCheck(ctx context.Context, database *db.DB, baseURL string, logger *slog.Logger) {
	runAIHealthCheck(ctx, database, baseURL, "", logger)
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

func runAIHealthCheck(ctx context.Context, database *db.DB, baseURL, externalToken string, logger *slog.Logger) {
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
	settings, err := database.GetAISettings(ctx)
	if err != nil {
		finishAIHealthTask(ctx, database, "failed", "读取 AI 计算节点设置失败", logger)
		return
	}
	nodes, err := ComputeNodes(settings, baseURL, externalToken)
	if err != nil {
		finishAIHealthTask(ctx, database, "failed", err.Error(), logger)
		return
	}
	healthy, failed := 0, make([]string, 0, len(nodes))
	for _, node := range nodes {
		status := ProbeComputeNode(ctx, node)
		if status.State == "online" || status.State == "paused" {
			healthy++
		} else {
			failed = append(failed, node.ID)
		}
	}
	if healthy == len(nodes) {
		finishAIHealthTask(ctx, database, "success", "全部 AI 计算节点运行正常", logger)
		return
	}
	if healthy > 0 {
		finishAIHealthTask(ctx, database, "warning", fmt.Sprintf("AI 节点 %s 不可用，已由另一节点继续处理", strings.Join(failed, "、")), logger)
		return
	}
	ubuntuURL := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	canRestartUbuntu := settings.ComputeMode == db.AIComputeModeUbuntu || settings.ComputeMode == db.AIComputeModeDual
	if canRestartUbuntu {
		restartErr := requestAIRestart(ctx, ubuntuURL)
		if waitForAIHealth(ctx, ubuntuURL, 3*time.Minute) {
			state := "success"
			message := "Ubuntu AI 服务已自动重启并恢复"
			if settings.ComputeMode == db.AIComputeModeDual {
				state = "warning"
				message = "Ubuntu AI 服务已恢复，外部节点仍不可用"
			}
			finishAIHealthTask(ctx, database, state, message, logger)
			return
		}
		if restartErr != nil {
			finishAIHealthTask(ctx, database, "failed", fmt.Sprintf("全部 AI 节点不可用，Ubuntu 重启请求失败：%v", restartErr), logger)
			return
		}
	}
	finishAIHealthTask(ctx, database, "failed", "全部 AI 计算节点均不可用", logger)
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
