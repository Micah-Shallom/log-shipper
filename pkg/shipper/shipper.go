package shipper

import (
	"fmt"
	"math"
	"net"
	"time"

	"github.com/micah-shallom/log-shipper/pkg/reader"
)

type LogShipper struct {
	serverHost string
	serverPort int
	logReader  *reader.LogReader
	maxRetries int
	conn       net.Conn
}

func NewLogShipper(serverHost string, serverPort int, logReader *reader.LogReader, maxRetries int) *LogShipper {
	return &LogShipper{
		serverHost: serverHost,
		serverPort: serverPort,
		logReader:  logReader,
		maxRetries: maxRetries,
	}
}

// establishes connection to the TCP server
func (ls *LogShipper) Connect() bool {
	address := net.JoinHostPort(ls.serverHost, fmt.Sprintf("%d", ls.serverPort))

	for retries := 0; retries < ls.maxRetries; retries++ {
		conn, err := net.Dial("tcp", address)
		if err == nil {
			ls.conn = conn
			fmt.Printf("connected to %s\n", address)
			return true
		}

		fmt.Printf("connection failed (attempt %d/%d): %v\n", retries+1, ls.maxRetries, err)

		backoffTime := time.Duration(math.Pow(2, float64(retries))) * time.Second
		time.Sleep(backoffTime)
	}

	fmt.Println("failed to connect after maximum retry attempts")
	return false
}

func (ls *LogShipper) Close() error {
	if ls.conn != nil {
		fmt.Println("Connection closed")
		err := ls.conn.Close()
		ls.conn = nil
		return err
	}
	return nil
}

func (ls *LogShipper) ShipLogsBatch(logs []string) (int, error) {
	if logs == nil {
		var err error
		logs, err = ls.logReader.ReadBatch()
		if err != nil {
			return 0, err
		}
	}

	if len(logs) == 0 {
		return 0, nil
	}

	if ls.conn == nil && !ls.Connect() {
		return 0, fmt.Errorf("failed to establish connection")
	}

	logsShipped := 0
	for _, log := range logs {
		if len(logs) > 0 && log[len(log)-1] != '\n' {
			log += "\n"
		}

		_, err := ls.conn.Write([]byte(log))
		if err != nil {
			fmt.Printf("error shipping log: %v\n", err)

			if ls.Connect() {
				_, err := ls.conn.Write([]byte(log))
				if err == nil {
					logsShipped++
				}
			}
			continue
		}
		logsShipped++
	}
	return logsShipped, nil
}

func (ls *LogShipper) ShipLogsContinuously(stopChan <-chan struct{}) error {
	if !ls.Connect() {
		return fmt.Errorf("could not establish intial connection")
	}

	logChan := make(chan string, 100)
	errChan := make(chan error, 1)

	//start reading logs in a seperate goroutine
	go func() {
		err := ls.logReader.ReadIncremental(logChan, stopChan)
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

			retryCount := 0
			sent := false

			for !sent && retryCount < ls.maxRetries {
				if len(log) > 0 && log[len(log)-1] != '\n' {
					log += "\n"
				}

				_, err := ls.conn.Write([]byte(log))
				if err == nil {
					sent = true
				}else {
					fmt.Printf("error shipping log: %v\n", err)
					retryCount++

					if ls.Connect(){
						continue
					} else {
						backoffTime := time.Duration(math.Pow(2, float64(retryCount))) * time.Second
						time.Sleep(backoffTime)
					}
				}
			}
		}
	}
}
