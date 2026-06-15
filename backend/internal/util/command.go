package util

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func RunCommand(ctx context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
	return runCommand(ctx, timeout, name, args...)
}

func RunLowPriorityCommand(ctx context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
	wrappedName, wrappedArgs := lowPriorityCommand(name, args)
	return runCommand(ctx, timeout, wrappedName, wrappedArgs...)
}

func runCommand(ctx context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
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

func lowPriorityCommand(name string, args []string) (string, []string) {
	if runtime.GOOS == "windows" {
		return name, args
	}
	nicePath, niceErr := exec.LookPath("nice")
	ionicePath, ioniceErr := exec.LookPath("ionice")
	niceLevel := commandIntEnv("BACKGROUND_NICE", 5)
	ioniceClass := commandIntEnv("BACKGROUND_IONICE_CLASS", 2)
	ioniceLevel := commandIntEnv("BACKGROUND_IONICE_LEVEL", 6)
	if niceErr == nil && ioniceErr == nil {
		wrapped := ioniceArgs(ioniceClass, ioniceLevel)
		wrapped = append(wrapped, nicePath, "-n", strconv.Itoa(niceLevel), name)
		wrapped = append(wrapped, args...)
		return ionicePath, wrapped
	}
	if niceErr == nil {
		wrapped := []string{"-n", strconv.Itoa(niceLevel), name}
		wrapped = append(wrapped, args...)
		return nicePath, wrapped
	}
	if ioniceErr == nil {
		wrapped := ioniceArgs(ioniceClass, ioniceLevel)
		wrapped = append(wrapped, name)
		wrapped = append(wrapped, args...)
		return ionicePath, wrapped
	}
	return name, args
}

func ioniceArgs(class int, level int) []string {
	if class < 1 || class > 3 {
		class = 2
	}
	args := []string{"-c", strconv.Itoa(class)}
	if class != 3 {
		if level < 0 {
			level = 0
		}
		if level > 7 {
			level = 7
		}
		args = append(args, "-n", strconv.Itoa(level))
	}
	return args
}

func commandIntEnv(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
