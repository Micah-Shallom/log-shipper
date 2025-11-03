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

	if metricsInt > 0 && !batch {
		metricsTicker := time.NewTicker(time.Duration(metricsInt) * time.Second)
		defer metricsTicker.Stop()

		go func() {
			for {
				select {
				case <-stopChan:
					return
				case <-metricsTicker.C:
					s.PrintMetrics()
				}
			}
		}()
	}

	if batch {
		count, err := s.ShipLogsBatch(nil)
		if err != nil {
			return err
		}
		fmt.Printf("Shipped %d logs in batch mode\n", count)
		s.PrintMetrics()
		return nil
	}

	compressionStatus := "Enabled"
	if !compress {
		compressionStatus = "Disabled"
	}

	fmt.Printf("Starting continuous log shipping (enhanced mode) from %s to %s:%d\n", logFile, serverHost, serverPort)
	fmt.Printf("Compression: %s, Batch Size: %d\n", compressionStatus, batchSize)
	fmt.Println("Press Ctrl+C to stop")
	return s.ShipLogsContinuously(stopChan)
}
