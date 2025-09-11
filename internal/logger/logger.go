package logger

import (
	"os"
	"strings"

	"github.com/sirupsen/logrus"
)

// InitLogger initializes and configures the logger
func InitLogger() *logrus.Logger {
	log := logrus.New()

	// Set log level
	level := strings.ToLower(os.Getenv("LOG_LEVEL"))
	switch level {
	case "debug":
		log.SetLevel(logrus.DebugLevel)
	case "info":
		log.SetLevel(logrus.InfoLevel)
	case "warn":
		log.SetLevel(logrus.WarnLevel)
	case "error":
		log.SetLevel(logrus.ErrorLevel)
	default:
		log.SetLevel(logrus.InfoLevel)
	}

	// Set log format
	format := strings.ToLower(os.Getenv("LOG_FORMAT"))
	if format == "json" {
		log.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: "2006-01-02T15:04:05.000Z",
		})
	} else {
		log.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: "2006-01-02T15:04:05.000Z",
		})
	}

	// Set output
	output := strings.ToLower(os.Getenv("LOG_OUTPUT"))
	if output == "stderr" {
		log.SetOutput(os.Stderr)
	} else {
		log.SetOutput(os.Stdout)
	}

	return log
}

// WithRequestID adds a request ID to the log context
func WithRequestID(log *logrus.Logger, requestID string) *logrus.Entry {
	return log.WithField("request_id", requestID)
}

// WithUploadContext adds upload-specific context to logs
func WithUploadContext(log *logrus.Logger, requestID, account, orgID string) *logrus.Entry {
	return log.WithFields(logrus.Fields{
		"request_id": requestID,
		"account":    account,
		"org_id":     orgID,
		"service":    "insights-ros-ingress",
	})
}