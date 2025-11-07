package cmd

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/micah-shallom/log-shipper/pkg/reader"
	"github.com/spf13/cobra"
)

var (
	logFiles        []string
	serverHost      string
	serverPort      int
	batch           bool
	bufferSize      int
	persistenceFile string
	compress        bool
	batchSize       int
	heartbeatInt    int
	metricsInt      int
	maxRetries      int
	shipperType     string
)

var rootCmd = &cobra.Command{
	Use:   "log-shipper",
	Short: "Ship logs to a remote TCP server",
	Long: `A log shipping client that sends logs to a remote TCP server.
Supports basic, resilient, and enhanced shipping modes with features like
buffering, compression, and metrics tracking.`,
	Run: runShipper,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().StringSliceVarP(&logFiles, "log-files", "f", []string{}, "Path to the log files to ship (required)")
	rootCmd.Flags().StringVarP(&serverHost, "server-host", "H", "localhost", "Host of the TCP log server")
	rootCmd.Flags().IntVarP(&serverPort, "server-port", "p", 9000, "Port of the TCP log server")
	rootCmd.Flags().BoolVarP(&batch, "batch", "b", false, "Ship logs in batch mode instead of continuous")
	rootCmd.Flags().IntVar(&bufferSize, "buffer-size", 1000, "Maximum logs to keep in memory buffer")
	rootCmd.Flags().StringVar(&persistenceFile, "persistence-file", "undelivered_logs.json", "File to store undelivered logs")
	rootCmd.Flags().BoolVar(&compress, "compress", true, "Compress logs before sending (enhanced mode only)")
	rootCmd.Flags().IntVar(&batchSize, "batch-size", 10, "Number of logs to batch together (enhanced mode only)")
	rootCmd.Flags().IntVar(&heartbeatInt, "heartbeat-interval", 30, "Seconds between heartbeat checks (0 to disable, enhanced mode only)")
	rootCmd.Flags().IntVar(&metricsInt, "metrics-interval", 60, "Seconds between metrics reports (0 to disable, enhanced mode only)")
	rootCmd.Flags().IntVar(&maxRetries, "max-retries", 5, "Maximum connection retry attempts")
	rootCmd.Flags().StringVarP(&shipperType, "type", "t", "enhanced", "Shipper type: basic, resilient, or enhanced")

	rootCmd.MarkFlagRequired("log-file")
}

func runShipper(cmd *cobra.Command, args []string) {
	if shipperType != "basic" && shipperType != "resilient" && shipperType != "enhanced" {
		fmt.Fprintf(os.Stderr, "Invalid shipper type: %s (must be basic, resilient, or enhanced)\n", shipperType)
		os.Exit(1)
	}

	var logReaders []*reader.LogReader
	logger := log.New(os.Stdout, "[LogCollector] ", log.LstdFlags)

	//create log reader
	logReader := reader.NewLogReader(logFiles, logger)
	defer logReader.Close()
	logReaders = append(logReaders, logReader)

	stopChan := make(chan struct{})
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nshutiing down log shipper...")
		close(stopChan)
	}()

	var err error

	switch shipperType {
	case "basic":
		err = runBasicShipper(logReaders, stopChan)
	case "resilient":
		err = runResilientShipper(logReaders, stopChan)
	case "enhanced":
		err = runEnhancedShipper(logReaders, stopChan)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error during log shipping")
	}
}
