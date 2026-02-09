package outputs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/msh100/modem-stats/utils"
)

// LokiExporter pushes log entries to a Loki endpoint
type LokiExporter struct {
	endpoint    string
	client      *http.Client
	seenLogs    map[string]bool
	seenLogsMu  sync.RWMutex
	labels      map[string]string
	logProvider utils.EventLogProvider
}

// lokiPushRequest represents the Loki push API request format
type lokiPushRequest struct {
	Streams []lokiStream `json:"streams"`
}

type lokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"`
}

// NewLokiExporter creates a new Loki exporter
func NewLokiExporter(endpoint string, logProvider utils.EventLogProvider, labels map[string]string) *LokiExporter {
	if labels == nil {
		labels = make(map[string]string)
	}
	if _, ok := labels["job"]; !ok {
		labels["job"] = "modem-stats"
	}

	return &LokiExporter{
		endpoint:    endpoint,
		client:      &http.Client{Timeout: 10 * time.Second},
		seenLogs:    make(map[string]bool),
		labels:      labels,
		logProvider: logProvider,
	}
}

// logKey generates a unique key for a log entry to track duplicates
func (l *LokiExporter) logKey(entry utils.EventLogEntry) string {
	return fmt.Sprintf("%s|%s|%s", entry.Timestamp, entry.Priority, entry.Message)
}

// PushLogs fetches new logs and pushes them to Loki
func (l *LokiExporter) PushLogs() error {
	entries, err := l.logProvider.FetchEventLog()
	if err != nil {
		return fmt.Errorf("failed to fetch event log: %w", err)
	}

	// Filter to only new entries
	var newEntries []utils.EventLogEntry
	l.seenLogsMu.RLock()
	for _, entry := range entries {
		key := l.logKey(entry)
		if !l.seenLogs[key] {
			newEntries = append(newEntries, entry)
		}
	}
	l.seenLogsMu.RUnlock()

	if len(newEntries) == 0 {
		return nil
	}

	// Group entries by priority (creates separate streams per priority level)
	streams := make(map[string][][]string)
	for _, entry := range newEntries {
		ts, err := time.Parse(time.RFC3339, entry.Timestamp)
		if err != nil {
			// If parsing fails, use current time
			ts = time.Now()
		}

		// Loki expects nanosecond timestamps as strings
		tsNano := fmt.Sprintf("%d", ts.UnixNano())
		streams[entry.Priority] = append(streams[entry.Priority], []string{tsNano, entry.Message})
	}

	// Build Loki push request
	var lokiStreams []lokiStream
	for priority, values := range streams {
		labels := make(map[string]string)
		for k, v := range l.labels {
			labels[k] = v
		}
		labels["level"] = priority

		// Sort values by timestamp (oldest first)
		sort.Slice(values, func(i, j int) bool {
			return values[i][0] < values[j][0]
		})

		lokiStreams = append(lokiStreams, lokiStream{
			Stream: labels,
			Values: values,
		})
	}

	req := lokiPushRequest{Streams: lokiStreams}
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal loki request: %w", err)
	}

	resp, err := l.client.Post(l.endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to push to loki: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("loki returned status %d", resp.StatusCode)
	}

	// Mark entries as seen after successful push
	l.seenLogsMu.Lock()
	for _, entry := range newEntries {
		l.seenLogs[l.logKey(entry)] = true
	}
	l.seenLogsMu.Unlock()

	log.Printf("Pushed %d log entries to Loki", len(newEntries))
	return nil
}

// StartPolling starts a background goroutine that polls for logs at the given interval
func (l *LokiExporter) StartPolling(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Initial push
		if err := l.PushLogs(); err != nil {
			log.Printf("Error pushing logs to Loki: %v", err)
		}

		for range ticker.C {
			if err := l.PushLogs(); err != nil {
				log.Printf("Error pushing logs to Loki: %v", err)
			}
		}
	}()
}
