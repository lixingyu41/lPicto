package util

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

func RunCommand(ctx context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if cmdCtx.Err() != nil {
		return nil, cmdCtx.Err()
	}
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, errors.New(message)
	}
	return out, nil
}
