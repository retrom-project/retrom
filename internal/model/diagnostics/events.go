package diagnostics

import (
	"context"
	"strings"
)

const CleanupFailureCode = "cleanup.failed"

// DiagnosticEvent contains only bounded, sanitized log facts, never an error or resource.
type DiagnosticEvent struct {
	Operation string
	Code      string
	Message   string
	RequestID string
}

// ErrorReporter is the external diagnostic sink. Repositories return event values.
type ErrorReporter interface {
	Report(context.Context, DiagnosticEvent)
}

// CleanupFailure classifies a nonfatal resource failure without inspecting the error.
// The resource owner supplies its outer Go type, never Error() or formatted error text.
func CleanupFailure(operation, requestID, errorType string) DiagnosticEvent {
	if !validOperation(operation) {
		operation = "cleanup"
	}
	if !validErrorType(errorType) {
		errorType = "error"
	}
	if !validRequestID(requestID) {
		requestID = ""
	}
	return DiagnosticEvent{
		Operation: operation, Code: CleanupFailureCode, Message: errorType, RequestID: requestID,
	}
}

func validOperation(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if !asciiLetter(char) && (char < '0' || char > '9') &&
			char != ' ' && char != '-' && char != '_' {
			return false
		}
	}
	return strings.TrimSpace(value) != ""
}

func validErrorType(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	if value == "error" {
		return true
	}
	value = strings.TrimPrefix(value, "*")
	parts := strings.Split(value, ".")
	return len(parts) == 2 && identifier(parts[0]) && identifier(parts[1])
}

func identifier(value string) bool {
	if value == "" {
		return false
	}
	for index, char := range value {
		if asciiLetter(char) || char == '_' || index > 0 && char >= '0' && char <= '9' {
			continue
		}
		return false
	}
	return true
}

func asciiLetter(char rune) bool {
	return char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z'
}

func validRequestID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, char := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if char != '-' {
				return false
			}
		} else if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
