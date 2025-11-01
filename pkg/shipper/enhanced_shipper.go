package shipper

import (
	"container/list"
	"net"
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
