package cmd

import (
	"fmt"
	"time"

	"github.com/micah-shallom/log-shipper/pkg/reader"
	"github.com/micah-shallom/log-shipper/pkg/shipper"
)

func runBasicShipper(logReader *reader.LogReader, stopChan chan struct{}) error {
	s := shipper.NewLogShipper(serverHost, serverPort, logReader, maxRetries)
	defer s.Close()

	if batch {
		count, err := s.ShipLogsBatch(nil)
		if err != nil {
			return err
		}
		fmt.Printf("shipped %d logs in batch mode\n", count)
		return nil
	}

	fmt.Printf("starting continous log shipping(basic mode) from %s to %s:%d", logFile, serverHost, serverPort)
	fmt.Println("press Ctrl+c to stop")
	return s.ShipLogsContinuously(stopChan)
}

func runResilientShipper(logReader *reader.LogReader, stopChan chan struct{}) error {
	s := shipper.NewResilientLogShipper(serverHost, serverPort, logReader, bufferSize, persistenceFile, maxRetries)
	defer s.Close()

	if batch {
		count, err := s.ShipLogsBatch(nil)
		if err != nil {
			return err
		}

		fmt.Printf("shipped %d logs in batch mode\n", count)
		return nil
	}

	fmt.Printf("starting continous log shipping (resilient mode) from %s to %s:%d \n", logFile, serverHost, serverPort)
	fmt.Println("press Ctrl+c to stop")
	return s.ShipLogsContinuously(stopChan)
}
func runEnhancedShipper(logReader *reader.LogReader, stopChan chan struct{}) error {
	heartbeatInterval := time.Duration(heartbeatInt) * time.Second
	s := shipper.NewEnhancedLogShipper(
		serverHost,
		serverPort,
		logReader,
		bufferSize,
		persistenceFile,
		maxRetries,
		compress,
		batchSize,
		heartbeatInterval,
	)
	defer s.Close()
}
