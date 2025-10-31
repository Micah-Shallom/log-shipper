package cmd

import (
	"fmt"

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
	return nil
}
func runEnhancedShipper(logReader *reader.LogReader, stopChan chan struct{}) error {
	return nil
}