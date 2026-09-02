package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"lpicto/backend/internal/db"
)

type ComputeNode struct {
	ID       string
	BaseURL  string
	External bool
	Token    string
}

type ComputeNodeStatus struct {
	ID        string `json:"id"`
	State     string `json:"state"`
	LatencyMS *int64 `json:"latencyMs"`
	CheckedAt int64  `json:"checkedAt"`
	Message   string `json:"message"`
}

const computeProtocolVersion = 1

func ComputeNodes(settings db.AISettings, ubuntuURL, externalToken string) ([]ComputeNode, error) {
	ubuntu := ComputeNode{ID: db.AIComputeModeUbuntu, BaseURL: strings.TrimRight(strings.TrimSpace(ubuntuURL), "/")}
	externalURL, externalErr := ExternalBaseURL(settings.ExternalHost, settings.ExternalPort)
	external := ComputeNode{ID: db.AIComputeModeExternal, BaseURL: externalURL, External: true, Token: strings.TrimSpace(externalToken)}
	switch settings.ComputeMode {
	case db.AIComputeModeUbuntu:
		if ubuntu.BaseURL == "" {
			return nil, fmt.Errorf("Ubuntu AI 地址未配置")
		}
		return []ComputeNode{ubuntu}, nil
	case db.AIComputeModeExternal:
		if externalErr != nil {
			return nil, externalErr
		}
		return []ComputeNode{external}, nil
	case db.AIComputeModeDual:
		if ubuntu.BaseURL == "" {
			return nil, fmt.Errorf("Ubuntu AI 地址未配置")
		}
		if externalErr != nil {
			return nil, externalErr
		}
		return []ComputeNode{ubuntu, external}, nil
	default:
		return nil, fmt.Errorf("AI 计算模式无效")
	}
}

func ExternalBaseURL(host string, port int) (string, error) {
	host = strings.TrimSpace(host)
	if net.ParseIP(host) == nil {
		return "", fmt.Errorf("外部 AI 必须填写有效 IP 地址")
	}
	if port < 1 || port > 65535 {
		return "", fmt.Errorf("外部 AI 端口必须为 1 到 65535")
	}
	return "http://" + net.JoinHostPort(host, fmt.Sprintf("%d", port)), nil
}

func ProbeComputeNode(ctx context.Context, node ComputeNode) ComputeNodeStatus {
	status := ComputeNodeStatus{ID: node.ID, State: "offline", CheckedAt: time.Now().Unix()}
	if strings.TrimSpace(node.BaseURL) == "" {
		status.State = "unconfigured"
		status.Message = "未配置"
		return status
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, strings.TrimRight(node.BaseURL, "/")+"/ready", nil)
	if err != nil {
		status.Message = err.Error()
		return status
	}
	setComputeNodeAuth(request, node)
	started := time.Now()
	response, err := http.DefaultClient.Do(request)
	elapsed := time.Since(started).Milliseconds()
	status.LatencyMS = &elapsed
	if err != nil {
		status.Message = err.Error()
		return status
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		status.Message = fmt.Sprintf("HTTP %d", response.StatusCode)
		return status
	}
	var body struct {
		Status          string `json:"status"`
		Service         string `json:"service"`
		ProtocolVersion int    `json:"protocolVersion"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil || body.Service != "lpicto-ai" || body.ProtocolVersion != computeProtocolVersion {
		status.Message = "节点协议不兼容"
		return status
	}
	switch strings.ToLower(strings.TrimSpace(body.Status)) {
	case "paused":
		status.State = "paused"
		status.Message = "已暂停"
		return status
	case "ok":
		status.State = "online"
		status.Message = "运行正常"
		return status
	default:
		status.Message = "健康检查响应无效"
		return status
	}
}

func PauseAllComputeNodes(ctx context.Context, settings db.AISettings, ubuntuURL, externalToken string) {
	nodes := make([]ComputeNode, 0, 2)
	if baseURL := strings.TrimRight(strings.TrimSpace(ubuntuURL), "/"); baseURL != "" {
		nodes = append(nodes, ComputeNode{ID: db.AIComputeModeUbuntu, BaseURL: baseURL})
	}
	if externalURL, err := ExternalBaseURL(settings.ExternalHost, settings.ExternalPort); err == nil {
		nodes = append(nodes, ComputeNode{ID: db.AIComputeModeExternal, BaseURL: externalURL, External: true, Token: strings.TrimSpace(externalToken)})
	}
	for _, node := range nodes {
		pauseCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		request, requestErr := http.NewRequestWithContext(pauseCtx, http.MethodPost, strings.TrimRight(node.BaseURL, "/")+"/pause", nil)
		if requestErr == nil {
			setComputeNodeAuth(request, node)
			if response, responseErr := http.DefaultClient.Do(request); responseErr == nil {
				response.Body.Close()
			}
		}
		cancel()
	}
}

func setComputeNodeAuth(request *http.Request, node ComputeNode) {
	if node.External && node.Token != "" {
		request.Header.Set("X-LPicto-AI-Token", node.Token)
	}
}
