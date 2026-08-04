// Package influxpush writes snapshots to an InfluxDB server.
//
// Writes happen on a background goroutine: a slow or unreachable InfluxDB must
// not stall the collection loop, and a reading that could not be delivered is
// dropped rather than queued, because by the time the server comes back the
// value would be stale anyway.
package influxpush

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/corgan2222/rig-exporter/internal/collector"
	"github.com/corgan2222/rig-exporter/internal/config"
	"github.com/corgan2222/rig-exporter/internal/export"
	"github.com/corgan2222/rig-exporter/internal/i18n"
	"github.com/corgan2222/rig-exporter/internal/metrics"
)

const writeTimeout = 10 * time.Second

// Client pushes line protocol to an InfluxDB v2 write endpoint.
type Client struct {
	cfg  config.Config
	log  *slog.Logger
	http *http.Client

	counter export.Counter

	// pending holds at most one snapshot. A newer reading replaces an
	// undelivered one, so the worker never falls behind.
	pending chan collector.Snapshot
	stop    chan struct{}
	done    chan struct{}
	once    sync.Once

	mu      sync.RWMutex
	lastErr string
	dropped uint64
	now     func() time.Time
}

// New builds the client. Nothing is sent until Start is called.
func New(cfg config.Config, log *slog.Logger) *Client {
	return &Client{
		cfg:     cfg,
		log:     log,
		http:    &http.Client{Timeout: writeTimeout},
		pending: make(chan collector.Snapshot, 1),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
		now:     time.Now,
	}
}

// Start launches the writer goroutine.
func (c *Client) Start() error {
	if c.cfg.InfluxURL == "" || c.cfg.InfluxBucket == "" {
		return fmt.Errorf("influx push needs a URL and a bucket")
	}

	go c.run()
	c.log.Info("influx push enabled",
		"url", c.cfg.InfluxURL, "bucket", c.cfg.InfluxBucket, "org", c.cfg.InfluxOrg)
	return nil
}

// Export queues a snapshot, replacing any reading that has not been written
// yet.
func (c *Client) Export(snap collector.Snapshot) error {
	select {
	case c.pending <- snap:
		return nil
	default:
	}

	// The worker is still busy with the previous reading. Swap it out for the
	// newer one instead of blocking the collection loop.
	select {
	case <-c.pending:
	default:
	}
	select {
	case c.pending <- snap:
	default:
		c.mu.Lock()
		c.dropped++
		c.mu.Unlock()
	}
	return nil
}

// Stop ends the writer goroutine.
func (c *Client) Stop() {
	c.once.Do(func() {
		close(c.stop)
		<-c.done
		c.log.Info("influx push stopped")
	})
}

// Status reports the write target and the last failure, if any.
func (c *Client) Status() export.Status {
	c.mu.RLock()
	lastErr, dropped := c.lastErr, c.dropped
	c.mu.RUnlock()

	lang := c.cfg.Lang()
	status := export.Status{
		Name:      "influx",
		Label:     i18n.T(lang, "export.influx"),
		Healthy:   lastErr == "",
		Delivered: c.counter.Count(),
		Detail:    fmt.Sprintf("%s → %s", c.cfg.InfluxURL, c.cfg.InfluxBucket),
	}
	if lastErr != "" {
		status.Detail = lastErr
	}
	if dropped > 0 {
		status.Detail += fmt.Sprintf(" (%d %s)", dropped, i18n.T(lang, "export.dropped"))
	}
	return status
}

func (c *Client) run() {
	defer close(c.done)

	for {
		select {
		case <-c.stop:
			return
		case snap := <-c.pending:
			if err := c.write(snap); err != nil {
				c.mu.Lock()
				c.lastErr = err.Error()
				c.mu.Unlock()
				c.log.Warn("influx write failed", "error", err)
				continue
			}
			c.mu.Lock()
			c.lastErr = ""
			c.mu.Unlock()
			c.counter.Inc()
		}
	}
}

func (c *Client) write(snap collector.Snapshot) error {
	body := snap.Influx(c.cfg.InfluxMeasurement, c.cfg.NodeID, c.now())

	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.InfluxWriteURL(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build write request: %w", err)
	}
	req.Header.Set("Content-Type", metrics.InfluxContentType)
	if c.cfg.InfluxToken != "" {
		// InfluxDB 2.x expects "Token <token>"; 1.8's compatibility API
		// accepts the same header with "user:password" as the token.
		req.Header.Set("Authorization", "Token "+c.cfg.InfluxToken)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("write to %s: %w", c.cfg.InfluxURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("influx returned %s: %s", resp.Status, bytes.TrimSpace(detail))
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return nil
}
