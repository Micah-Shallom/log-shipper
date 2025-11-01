package shipper

import (
	"container/list"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/micah-shallom/log-shipper/pkg/reader"
)

type ResilientLogShipper struct {
	serverHost      string
	serverPort      int
	logReader       *reader.LogReader
	bufferSize      int
	persistenceFile string
	maxRetries      int
	conn            net.Conn
	buffer          *list.List
}

func NewResilientLogShipper(serverHost string, serverPort int, logReader *reader.LogReader, bufferSize int, persistenceFile string, maxRetries int) *ResilientLogShipper {
	rs := &ResilientLogShipper{
		serverHost:      serverHost,
		serverPort:      serverPort,
		logReader:       logReader,
		bufferSize:      bufferSize,
		persistenceFile: persistenceFile,
		maxRetries:      maxRetries,
		buffer:          list.New(),
	}

	rs.loadPersistedLogs()
	return rs
}

// loads any logs that werent delivered in previous runs
func (rs *ResilientLogShipper) loadPersistedLogs() {
	if rs.persistenceFile == "" {
		return
	}

	data, err := os.ReadFile(rs.persistenceFile)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Printf("error loading persisted logs: %v\n", err)
		}
	}

	var persistedLogs []string
	if err := json.Unmarshal(data, &persistedLogs); err != nil {
		fmt.Printf("error parsing persisted logs: %v\n", err)
		return
	}

	for _, log := range persistedLogs {
		rs.buffer.PushBack(log)
	}

	fmt.Printf("Loaded %d persisted logs\n", len(persistedLogs))

	//clear the persistence file
	os.Remove(rs.persistenceFile)
}

// saves any undelievered logs to disk
func (rs *ResilientLogShipper) persistUndelieveredLogs() {
	if rs.persistenceFile == "" || rs.buffer.Len() == 0 {
		return
	}

	logs := make([]string, 0, rs.buffer.Len())
	for e := rs.buffer.Front(); e != nil; e = e.Next() {
		logs = append(logs, e.Value.(string))
	}

	data, err := json.MarshalIndent(logs, "", " ")
	if err != nil {
		fmt.Printf("error marshaling logs:")
	}

	if err := os.WriteFile(rs.persistenceFile, data, 0644); err != nil {
		fmt.Printf("error persisting logs: %v\n", err)
		return
	}

	fmt.Printf("persisted %d undelivered logs\n", len(logs))

}

func (rs *ResilientLogShipper) Close() error {
	//try sending any remaining buffered logs
	if rs.buffer.Len() > 0 {
		fmt.Printf("attempting to send %d buffered logs before shutdown\n", rs.buffer.Len())
		rs.processBuffer()
	}

	rs.persistUndelieveredLogs()

	if rs.conn != nil {
		fmt.Println("connection closed")
		err := rs.conn.Close()
		rs.conn = nil
		return err
	}

	return nil
}

// processbuffer attempts to send any logs in the buffer
func (rs *ResilientLogShipper) processBuffer() int {
	if rs.buffer.Len() == 0 {
		return 0
	}

	if rs.conn == nil && !rs.Connect() {
		return 0
	}

	logsSent := 0
	bufferSize := rs.buffer.Len()

	for range bufferSize {
		if rs.buffer.Len() == 0 {
			break
		}

		e := rs.buffer.Front()
		log := e.Value.(string)

		if len(log) > 0 && log[len(log)-1] != '\n' {
			log += "\n"
		}

		_, err := rs.conn.Write([]byte(log))
		if err != nil {
			fmt.Printf("error shipping log from buffer: %v\n", err)

			if !rs.Connect() {
				break
			}
			continue
		}

		rs.buffer.Remove(e)
		logsSent++
	}

	return logsSent
}

func (rs *ResilientLogShipper) Connect() bool {
	address := net.JoinHostPort(rs.serverHost, strconv.Itoa(rs.serverPort))

	for retries := 0; retries < rs.maxRetries; retries++ {
		conn, err := net.Dial("tcp", address)
		if err == nil {
			rs.conn = conn
			fmt.Printf("connected to %s\n", address)
			return true
		}

		fmt.Printf("connection failed (attempt %d/%d): %v\n", retries+1, rs.maxRetries, err)

		backoffTime := time.Duration(math.Min(60, math.Pow(2, float64(retries)))) * time.Second //capped at 60seconds
		fmt.Printf("retrying in %v...\n", backoffTime)
		time.Sleep(backoffTime)
	}

	fmt.Println("failed to connect after maximum retry attempts")
	return false
}

func (rs *ResilientLogShipper) ShipLogsBatch(logs []string) (int, error) {
	bufferSent := rs.processBuffer()

	if logs == nil {
		var err error
		logs, err = rs.logReader.ReadBatch()
		if err != nil {
			return bufferSent, err
		}
	}

	if len(logs) == 0 {
		return bufferSent, nil
	}

	if rs.conn == nil && !rs.Connect() {
		for _, log := range logs {
			if rs.buffer.Len() < rs.bufferSize {
				rs.buffer.PushBack(log)
			}
		}

		return bufferSent, nil
	}

	logsShipped := bufferSent
	for _, log := range logs {
		if len(log) > 0 && log[len(log)-1] != '\n' {
			log += "\n"
		}

		_, err := rs.conn.Write([]byte(log))
		if err != nil {
			fmt.Printf("error shipping log: %v\n", err)

			if rs.buffer.Len() < rs.bufferSize {
				rs.buffer.PushBack(log)
			}

			if !rs.Connect() {
				for _, remainingLog := range logs[logsShipped-bufferSent:] {
					if rs.buffer.Len() < rs.bufferSize {
						rs.buffer.PushBack(remainingLog)
					}
				}
				break
			}
			continue
		}
		logsShipped++
	}
	return logsShipped, nil
}

func (rs *ResilientLogShipper) ShipLogsContinuously(stopChan <-chan struct{}) error {
	rs.processBuffer()

	if rs.conn == nil && !rs.Connect() {
		fmt.Println("could not establish initial connection, will retry")
	}

	logChan := make(chan string, 100)
	errChan := make(chan error, 1)

	go func() {
		err := rs.logReader.ReadIncremental(logChan, stopChan)
		if err != nil {
			errChan <- err
		}
		close(logChan)
	}()

	for {
		select {
		case <-stopChan:
			return nil
		case err := <-errChan:
			return err
		case log, ok := <-logChan:
			if !ok {
				return nil
			}

			rs.processBuffer()

			if len(log) > 0 && log[len(log)-1] != '\n' {
				log += "\n"
			}

			if rs.conn != nil {
				_, err := rs.conn.Write([]byte(log))
				if err != nil {
					fmt.Printf("error shipping log: %v\n", err)

					if rs.buffer.Len() < rs.bufferSize{
						rs.buffer.PushBack(log)
					}

					rs.Connect()
				} 
			} else {
				if rs.buffer.Len() < rs.bufferSize {
					rs.buffer.PushBack(log)
				}

				rs.Connect()
			}
		}
	}
}
