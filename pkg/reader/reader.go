package reader

import (
	"bufio"
	"fmt"
	"os"
	"time"
)

type LogReader struct {
	logFilePath  string
	lastPosition int64
	file         *os.File
}

func NewLogReader(logFilePath string) *LogReader {
	return &LogReader{
		logFilePath:  logFilePath,
		lastPosition: 0,
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

	fmt.Printf("read %d logs from file %s", len(lines), lr.logFilePath)
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
