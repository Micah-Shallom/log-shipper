package reader

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync"
)

type LogFileHandler struct {
	FilePath     string
	LastPosition int64
	Mu           sync.Mutex
	Logger       *log.Logger
}

func NewLogFileHandler(filePath string, logger *log.Logger) (*LogFileHandler, error) {
	handler := &LogFileHandler{
		FilePath:     filePath,
		LastPosition: 0,
		Logger:       logger,
	}

	if err := handler.initializePosition(); err != nil {
		return nil, fmt.Errorf("failed to initialize position: %w", err)
	}

	return handler, nil
}

func (h *LogFileHandler) initializePosition() error {
	h.Mu.Lock()
	defer h.Mu.Unlock()

	file, err := os.Open(h.FilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
	}
	defer file.Close()

	pos, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}

	h.LastPosition = pos
	return nil
}
