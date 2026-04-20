package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	apitypes "ben/desktop/api/types"
	"ben/desktop/internal/desktopcore"
)

type coreRuntimeLogger struct{}

func newCoreRuntimeLogger() apitypes.Logger {
	return coreRuntimeLogger{}
}

func (coreRuntimeLogger) Printf(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	log.Print(message)
	recordRuntimeNetworkLog("info", message)
}

func (coreRuntimeLogger) Errorf(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	log.Print(message)
	recordRuntimeNetworkLog("error", message)
}

func recordRuntimeNetworkLog(level, message string) {
	if !shouldRecordRuntimeNetworkLog(message) {
		return
	}
	desktopcore.RecordNetworkDebugTrace(apitypes.NetworkDebugTraceEntry{
		TimestampMS: time.Now().UTC().UnixMilli(),
		Level:       strings.TrimSpace(level),
		Kind:        "runtime.log",
		Message:     strings.TrimSpace(message),
	})
}

func shouldRecordRuntimeNetworkLog(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	if message == "" || !strings.Contains(message, "desktopcore:") {
		return false
	}
	keywords := []string{
		"peer",
		"sync",
		"transport",
		"mdns",
		"relay",
		"dial",
		"library change",
		"checkpoint",
		"invite",
		"connected",
		"disconnected",
	}
	for _, keyword := range keywords {
		if strings.Contains(message, keyword) {
			return true
		}
	}
	return false
}
