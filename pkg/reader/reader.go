package reader

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

type LogReader struct {
	logFilePaths []string
	lastPosition int64
	file         *os.File
	handlers     []*LogFileHandler
	watcher      *fsnotify.Watcher
	logger       *log.Logger
}

func NewLogReader(logFilePaths []string, logger *log.Logger) *LogReader {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logger.Printf("failed to create watcher: %w\n", err)
		return nil
	}
	var handlers []*LogFileHandler

	for _, logFilePath := range logFilePaths {
		absPath, err := filepath.Abs(logFilePath)
		if err != nil {
			logger.Printf("failed to get absolute path: %w\n", err)
			return nil
		}

		logDir := filepath.Dir(absPath)
		if err := os.MkdirAll(logDir, 0755); err != nil {
			logger.Printf("failed to create directory %s: %w", absPath, err)
			return nil
		}

		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			file, err := os.Create(absPath)
			if err != nil {
				logger.Printf("failed to create log file %s: %w", absPath, err)
				return nil
			}
			file.Close()
			logger.Printf("createed empty log file at %s\n", absPath)
		}

		handler, err := NewLogFileHandler(absPath, logger)
		if err != nil {
			logger.Printf("failed to create log file handler: %w\n", err)
			return nil
		}
		handlers = append(handlers, handler)

		if err := watcher.Add(logDir); err != nil {
			logger.Printf("failed to add watcher to directory %s: %w", logDir, err)
			return nil
		}
	}

	return &LogReader{
		logFilePaths: logFilePaths,
		lastPosition: 0,
		watcher:      watcher,
		logger:       logger,
		handlers:     handlers,
	}
}

func (lr *LogReader) Close() error {
	if lr.file != nil {
		return lr.file.Close()
	}
	return nil
}

func (lr *LogReader) ReadBatch() ([]string, error) {
	file, err := os.Open(lr.logFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("WARNING: log file %s not found\n", lr.logFilePath)
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading log file: %w", err)
	}

	return lines, nil
}

func (lr *LogReader) ReadIncremental(logChan chan<- string, stopChan <-chan struct{}) error {
	file, err := os.Open(lr.logFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("Warning: log file %s not found\n", lr.logFilePath)
			return nil
		}
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	lr.file = file

	//move to the last read position
	if lr.lastPosition > 0 {
		_, err := file.Seek(lr.lastPosition, 0)
		if err != nil {
			return fmt.Errorf("failed to seek to position: %w", err)
		}
	}

	scanner := bufio.NewScanner(file)
	for {
		select {
		case <-stopChan:
			return nil
		default:
			if scanner.Scan() {
				line := scanner.Text()
				fmt.Println("log sent to channel")
				logChan <- line
				lr.lastPosition, _ = file.Seek(0, 1) // get current position
			} else {
				if err := scanner.Err(); err != nil {
					return fmt.Errorf("error reading log file: %w", err)
				}
				time.Sleep(100 * time.Millisecond)

				if _, err := os.Stat(lr.logFilePath); os.IsNotExist(err) {
					fmt.Printf("warning: log file %s disappeared\n", lr.logFilePath)
					return nil
				}
			}
		}
	}

}
