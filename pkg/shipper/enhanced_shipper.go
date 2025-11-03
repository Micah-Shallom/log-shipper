package shipper

import (
	"bytes"
	"compress/gzip"
	"container/list"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"sync"
	"time"

	"github.com/micah-shallom/log-shipper/pkg/reader"
)

type Metrics struct {
	LogsSent                int64
	LogsFailed              int64
	BytesSent               int64
	BytesSavedbyCompression int64
	ConnectionAttempts      int64
	ConnectionFailures      int64
	HeartbeatsSent          int64
	HeartbeatsFailed        int64
	AverageLatencyMs        float64
	TotalLatencySamples     int64
	mutex                   sync.RWMutex
}

func (m *Metrics) GetSnapshot() Metrics {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return Metrics{
		LogsSent:                m.LogsSent,
		LogsFailed:              m.LogsFailed,
		BytesSent:               m.BytesSent,
		BytesSavedbyCompression: m.BytesSavedbyCompression,
		ConnectionAttempts:      m.ConnectionAttempts,
		ConnectionFailures:      m.ConnectionFailures,
		HeartbeatsSent:          m.HeartbeatsSent,
		HeartbeatsFailed:        m.HeartbeatsFailed,
		AverageLatencyMs:        m.AverageLatencyMs,
		TotalLatencySamples:     m.TotalLatencySamples,
	}
}

type EnhancedLogShipper struct {
	serverHost        string
	serverPort        int
	logReader         *reader.LogReader
	bufferSize        int
	persistenceFile   string
	maxRetries        int
	compressLogs      bool
	batchSize         int
	heartbeatInterval time.Duration
	conn              net.Conn
	buffer            *list.List
	metrics           *Metrics
	running           bool
	stopHeartbeat     chan struct{}
}

func NewEnhancedLogShipper(serverHost string, serverPort int, logReader *reader.LogReader,
	bufferSize int, persistenceFile string, maxRetries int, compressLogs bool,
	batchSize int, heartbeatInterval time.Duration) *EnhancedLogShipper {

	es := &EnhancedLogShipper{
		serverHost:        serverHost,
		serverPort:        serverPort,
		logReader:         logReader,
		bufferSize:        bufferSize,
		persistenceFile:   persistenceFile,
		maxRetries:        maxRetries,
		compressLogs:      compressLogs,
		batchSize:         batchSize,
		heartbeatInterval: heartbeatInterval,
		buffer:            list.New(),
		metrics:           &Metrics{},
		running:           true,
		stopHeartbeat:     make(chan struct{}),
	}

	es.loadPersistedLogs()

	if heartbeatInterval > 0 {
		go es.heartbeatLoop()
	}

	return es
}

func (es *EnhancedLogShipper) loadPersistedLogs() {
	if es.persistenceFile == "" {
		return
	}

	data, err := os.ReadFile(es.persistenceFile)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Printf("Error loading persisted logs: %v\n", err)
		}
		return
	}

	var persistedLogs []string
	if err := json.Unmarshal(data, &persistedLogs); err != nil {
		fmt.Printf("Error parsing persisted logs: %v\n", err)
		return
	}

	for _, log := range persistedLogs {
		es.buffer.PushBack(log)
	}

	fmt.Printf("Loaded %d persisted logs\n", len(persistedLogs))
	os.Remove(es.persistenceFile)
}

func (es *EnhancedLogShipper) heartbeatLoop() {
	ticker := time.NewTicker(es.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-es.stopHeartbeat:
			return
		case <-ticker.C:
			es.sendHeartbeat()
		}
	}
}

func (es *EnhancedLogShipper) sendHeartbeat() bool {
	if es.conn == nil {
		if !es.Connect() {
			es.metrics.mutex.Lock()
			es.metrics.HeartbeatsFailed++
			es.metrics.mutex.Unlock()
			return false
		}
	}

	start := time.Now()
	es.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))

	_, err := es.conn.Write([]byte("HEARTBEAT\n"))

	es.conn.SetWriteDeadline(time.Time{}) //reset timeout

	if err != nil {
		es.metrics.mutex.Lock()
		es.metrics.HeartbeatsFailed++
		es.metrics.mutex.Unlock()
		es.conn = nil
		return false
	}

	//update metrics
	latencyMs := float64(time.Since(start).Microseconds()) / 1000.0

	es.metrics.mutex.Lock()
	totalLatency := es.metrics.AverageLatencyMs * float64(es.metrics.TotalLatencySamples)
	es.metrics.TotalLatencySamples++
	es.metrics.AverageLatencyMs = (totalLatency + latencyMs) / float64(es.metrics.TotalLatencySamples)
	es.metrics.HeartbeatsSent++
	es.metrics.mutex.Unlock()

	return true
}

func (es *EnhancedLogShipper) Connect() bool {
	es.metrics.mutex.Lock()
	es.metrics.ConnectionAttempts++
	es.metrics.mutex.Unlock()

	address := fmt.Sprintf("%s:%d", es.serverHost, es.serverPort)

	for retries := 0; retries < es.maxRetries; retries++ {
		conn, err := net.Dial("tcp", address)
		if err == nil {
			es.conn = conn
			fmt.Printf("Connected to %s\n", address)
			return true
		}

		fmt.Printf("Connection failed (attempt %d/%d): %v\n", retries+1, es.maxRetries, err)

		backoffTime := time.Duration(math.Min(60, math.Pow(2, float64(retries)))) * time.Second
		time.Sleep(backoffTime)
	}

	fmt.Println("Failed to connect after maximum retry attempts")

	es.metrics.mutex.Lock()
	es.metrics.ConnectionFailures++
	es.metrics.mutex.Unlock()

	return false
}

func (es *EnhancedLogShipper) Close() error {
	es.running = false
	close(es.stopHeartbeat)

	// Try to send any remaining buffered logs
	if es.buffer.Len() > 0 {
		fmt.Printf("Attempting to send %d buffered logs before shutdown\n", es.buffer.Len())
		es.processBufferInBatches()
	}

	// Print final metrics
	es.PrintMetrics()

	// Persist any logs that couldn't be delivered
	es.persistUndeliveredLogs()

	// Close the connection
	if es.conn != nil {
		fmt.Println("Connection closed")
		err := es.conn.Close()
		es.conn = nil
		return err
	}
	return nil
}

func (es *EnhancedLogShipper) processBufferInBatches() int {
	if es.buffer.Len() == 0 {
		return 0
	}

	if es.conn == nil && !es.Connect() {
		return 0
	}

	logsSent := 0

	for es.buffer.Len() > 0 {
		batch := make([]string, 0, es.batchSize)
		batchElements := make([]*list.Element, 0, es.batchSize)

		for i := 0; i < es.batchSize && es.buffer.Len() > 0; i++ {
			e := es.buffer.Front()
			batch = append(batch, e.Value.(string))
			batchElements = append(batchElements, e)
		}

		sent := es.sendBatch(batch)

		if sent == 0 {
			break
		}

		for i := 0; i < sent; i++ {
			es.buffer.Remove(batchElements[i])
		}

		logsSent += sent
	}

	return logsSent
}

func (es *EnhancedLogShipper) sendBatch(logs []string) int {
	if len(logs) == 0 {
		return 0
	}

	if es.conn == nil && !es.Connect() {
		return 0
	}

	start := time.Now()

	var err error
	var bytesSent int64

	if es.compressLogs {
		// Send compressed batch with header
		compressedData, compressErr := es.compressLogsBatch(logs)
		if compressErr != nil {
			fmt.Printf("Error compressing logs: %v\n", compressErr)
			es.metrics.mutex.Lock()
			es.metrics.LogsFailed += int64(len(logs))
			es.metrics.mutex.Unlock()
			return 0
		}

		// Send header
		_, err = es.conn.Write([]byte("COMPRESSED\n"))
		if err == nil {
			// Send length
			lengthStr := fmt.Sprintf("%d\n", len(compressedData))
			_, err = es.conn.Write([]byte(lengthStr))
			if err == nil {
				// Send compressed data
				_, err = es.conn.Write(compressedData)
				bytesSent = int64(len(compressedData) + len(lengthStr) + 11)
			}
		}
	} else {
		// Send each log individually
		var batchData bytes.Buffer
		for _, log := range logs {
			if len(log) > 0 && log[len(log)-1] != '\n' {
				batchData.WriteString(log + "\n")
			} else {
				batchData.WriteString(log)
			}
		}

		_, err = es.conn.Write(batchData.Bytes())
		bytesSent = int64(batchData.Len())
	}

	if err != nil {
		fmt.Printf("Error sending batch: %v\n", err)
		es.metrics.mutex.Lock()
		es.metrics.LogsFailed += int64(len(logs))
		es.metrics.mutex.Unlock()
		es.conn = nil
		return 0
	}

	// Update metrics
	latencyMs := float64(time.Since(start).Microseconds()) / 1000.0

	es.metrics.mutex.Lock()
	totalLatency := es.metrics.AverageLatencyMs * float64(es.metrics.TotalLatencySamples)
	es.metrics.TotalLatencySamples++
	es.metrics.AverageLatencyMs = (totalLatency + latencyMs) / float64(es.metrics.TotalLatencySamples)
	es.metrics.LogsSent += int64(len(logs))
	es.metrics.BytesSent += bytesSent
	es.metrics.mutex.Unlock()

	return len(logs)
}

func (es *EnhancedLogShipper) compressLogsBatch(logs []string) ([]byte, error) {
	// Join logs with newlines
	var combined bytes.Buffer
	for _, log := range logs {
		if len(log) > 0 && log[len(log)-1] != '\n' {
			combined.WriteString(log + "\n")
		} else {
			combined.WriteString(log)
		}
	}

	combinedBytes := combined.Bytes()

	// Compress
	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)

	if _, err := gzipWriter.Write(combinedBytes); err != nil {
		return nil, err
	}

	if err := gzipWriter.Close(); err != nil {
		return nil, err
	}

	compressedData := compressed.Bytes()

	// Update metrics
	es.metrics.mutex.Lock()
	es.metrics.BytesSavedbyCompression += int64(len(combinedBytes) - len(compressedData))
	es.metrics.mutex.Unlock()

	return compressedData, nil
}

func (es *EnhancedLogShipper) GetMetrics() map[string]interface{} {
	snapshot := es.metrics.GetSnapshot()

	bufferUtilization := 0.0
	if es.bufferSize > 0 {
		bufferUtilization = float64(es.buffer.Len()) / float64(es.bufferSize)
	}

	return map[string]interface{}{
		"logs_sent":                  snapshot.LogsSent,
		"logs_failed":                snapshot.LogsFailed,
		"bytes_sent":                 snapshot.BytesSent,
		"bytes_saved_by_compression": snapshot.BytesSavedbyCompression,
		"average_latency_ms":         snapshot.AverageLatencyMs,
		"connection_attempts":        snapshot.ConnectionAttempts,
		"connection_failures":        snapshot.ConnectionFailures,
		"heartbeats_sent":            snapshot.HeartbeatsSent,
		"heartbeats_failed":          snapshot.HeartbeatsFailed,
		"buffer_utilization":         bufferUtilization,
		"buffer_size":                es.buffer.Len(),
		"buffer_capacity":            es.bufferSize,
	}
}

func (es *EnhancedLogShipper) PrintMetrics() {
	metrics := es.GetMetrics()

	fmt.Println("\n=== Log Shipping Metrics ===")
	fmt.Printf("Logs Sent: %d\n", metrics["logs_sent"])
	fmt.Printf("Logs Failed: %d\n", metrics["logs_failed"])
	fmt.Printf("Bytes Sent: %d bytes\n", metrics["bytes_sent"])
	fmt.Printf("Bytes Saved by Compression: %d bytes\n", metrics["bytes_saved_by_compression"])
	fmt.Printf("Average Latency: %.2f ms\n", metrics["average_latency_ms"])
	fmt.Printf("Connection Attempts: %d\n", metrics["connection_attempts"])
	fmt.Printf("Connection Failures: %d\n", metrics["connection_failures"])
	fmt.Printf("Heartbeats Sent: %d\n", metrics["heartbeats_sent"])
	fmt.Printf("Heartbeats Failed: %d\n", metrics["heartbeats_failed"])
	fmt.Printf("Buffer Utilization: %.1f%% (%d/%d)\n",
		metrics["buffer_utilization"].(float64)*100,
		metrics["buffer_size"],
		metrics["buffer_capacity"])
	fmt.Println("===========================\n")
}

func (es *EnhancedLogShipper) persistUndeliveredLogs() {
	if es.persistenceFile == "" || es.buffer.Len() == 0 {
		return
	}

	logs := make([]string, 0, es.buffer.Len())
	for e := es.buffer.Front(); e != nil; e = e.Next() {
		logs = append(logs, e.Value.(string))
	}

	data, err := json.MarshalIndent(logs, "", "  ")
	if err != nil {
		fmt.Printf("Error marshaling logs: %v\n", err)
		return
	}

	if err := os.WriteFile(es.persistenceFile, data, 0644); err != nil {
		fmt.Printf("Error persisting logs: %v\n", err)
		return
	}

	fmt.Printf("Persisted %d undelivered logs\n", len(logs))
}

func (es *EnhancedLogShipper) ShipLogsBatch(logs []string) (int, error) {
	bufferSent := es.processBufferInBatches()

	if logs == nil {
		var err error
		logs, err = es.logReader.ReadBatch()
		if err != nil {
			return bufferSent, err
		}
	}

	if len(logs) == 0 {
		return bufferSent, nil
	}

	if es.conn == nil && !es.Connect() {
		for _, log := range logs {
			if es.buffer.Len() < es.bufferSize {
				es.buffer.PushBack(log)
			}
		}
		return bufferSent, nil
	}

	logsShipped := bufferSent
	for i := 0; i < len(logs); i += es.batchSize {
		end := i + es.batchSize
		if end > len(logs) {
			end = len(logs)
		}
		batch := logs[i:end]

		sent := es.sendBatch(batch)

		if sent == 0 {
			for j := i; j < len(logs); j++ {
				if es.buffer.Len() < es.bufferSize {
					es.buffer.PushBack(logs[j])
				}
			}
			break
		}

		logsShipped += sent
	}

	return logsShipped, nil
}

func (es *EnhancedLogShipper) ShipLogsContinuously(stopChan <-chan struct{}) error {
	es.processBufferInBatches()

	if es.conn == nil && !es.Connect() {
		fmt.Println("Could not establish initial connection, will retry")
	}

	logChan := make(chan string, 100)
	errChan := make(chan error, 1)

	go func() {
		err := es.logReader.ReadIncremental(logChan, stopChan)
		if err != nil {
			errChan <- err
		}
		close(logChan)
	}()

	currentBatch := make([]string, 0, es.batchSize)

	for {
		select {
		case <-stopChan:
			if len(currentBatch) > 0 {
				if es.conn != nil {
					sent := es.sendBatch(currentBatch)
					if sent == 0 {
						for _, log := range currentBatch {
							if es.buffer.Len() < es.bufferSize {
								es.buffer.PushBack(log)
							}
						}
					}
				} else {
					for _, log := range currentBatch {
						if es.buffer.Len() < es.bufferSize {
							es.buffer.PushBack(log)
						}
					}
				}
			}
			return nil
		case err := <-errChan:
			return err
		case log, ok := <-logChan:
			if !ok {
				if len(currentBatch) > 0 {
					if es.conn != nil {
						sent := es.sendBatch(currentBatch)
						if sent == 0 {
							for _, l := range currentBatch {
								if es.buffer.Len() < es.bufferSize {
									es.buffer.PushBack(l)
								}
							}
						}
					}
				}
				return nil
			}

			// Add to current batch
			currentBatch = append(currentBatch, log)

			// If we've reached batch size, send the batch
			if len(currentBatch) >= es.batchSize {
				// Try to process any buffered logs first
				es.processBufferInBatches()

				// Try to send the current batch
				if es.conn != nil {
					sent := es.sendBatch(currentBatch)
					if sent == 0 {
						// Failed to send, buffer the batch
						for _, l := range currentBatch {
							if es.buffer.Len() < es.bufferSize {
								es.buffer.PushBack(l)
							}
						}

						// Try to reconnect
						es.Connect()
					}
				} else {
					// No connection, buffer the batch
					for _, l := range currentBatch {
						if es.buffer.Len() < es.bufferSize {
							es.buffer.PushBack(l)
						}
					}

					// Try to reconnect
					es.Connect()
				}

				// Clear the batch
				currentBatch = make([]string, 0, es.batchSize)
			}
		}
	}
}
