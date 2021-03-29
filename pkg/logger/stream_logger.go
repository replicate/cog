package logger

import (
	"encoding/json"
	"fmt"
	"net/http"

	log "github.com/sirupsen/logrus"

	"github.com/replicate/cog/pkg/model"
)

type MessageType string

const (
	MessageTypeLogLine   MessageType = "log_line"
	MessageTypeDebugLine MessageType = "debug_line"
	MessageTypeError     MessageType = "error"
	MessageTypeStatus    MessageType = "status"
	MessageTypeModel     MessageType = "model"
)

type Message struct {
	Type  MessageType  `json:"type"`
	Text  string       `json:"data"`
	Model *model.Model `json:"model"`
}

type StreamLogger struct {
	writer http.ResponseWriter
}

func NewStreamLogger(w http.ResponseWriter) *StreamLogger {
	return &StreamLogger{writer: w}
}

func (logger *StreamLogger) logText(messageType MessageType, text string) {
	msg := Message{Type: messageType, Text: text}
	data, err := json.Marshal(msg)
	if err != nil {
		log.Warnf("Failed to marshal log text: %s", text)
	}
	logger.write(data)
}

func (logger *StreamLogger) write(data []byte) {
	data = append(data, '\n')
	if _, err := logger.writer.Write(data); err != nil {
		log.Warnf("HTTP response writer failed to write: %s", data)
		return
	}
	if f, ok := logger.writer.(http.Flusher); ok {
		f.Flush()
	} else {
		log.Warnf("HTTP response writer can not be flushed")
	}
}

func (logger *StreamLogger) WriteLogLine(line string, args ...interface{}) {
	logger.logText(MessageTypeLogLine, fmt.Sprintf(line, args...))
}

func (logger *StreamLogger) WriteDebugLine(line string, args ...interface{}) {
	logger.logText(MessageTypeDebugLine, fmt.Sprintf(line, args...))
}

func (logger *StreamLogger) WriteStatus(status string, args ...interface{}) {
	logger.logText(MessageTypeStatus, fmt.Sprintf(status, args...))
}

func (logger *StreamLogger) WriteError(err error) {
	logger.logText(MessageTypeError, err.Error())
}

func (logger *StreamLogger) WriteModel(mod *model.Model) {
	msg := Message{Type: MessageTypeModel, Model: mod}
	data, err := json.Marshal(msg)
	if err != nil {
		log.Warnf("Failed to marshal model")
	}
	logger.write(data)
}
