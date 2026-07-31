//go:build windows

package app

import (
	"testing"

	"github.com/corgan/rig-exporter/internal/collector"
	"github.com/corgan/rig-exporter/internal/metrics"
)

// closingSource stands in for the processor source, which holds a PDH query
// open for as long as the set exists.
type closingSource struct{ closed int }

func (c *closingSource) Group() metrics.Group           { return metrics.GroupCPU }
func (c *closingSource) Collect(set *metrics.Set) error { return nil }
func (c *closingSource) Close()                         { c.closed++ }

type plainSource struct{}

func (plainSource) Group() metrics.Group           { return metrics.GroupDisk }
func (plainSource) Collect(set *metrics.Set) error { return nil }

// Saving the settings throws the whole set away and builds a new one, so a
// source that owns an operating system handle has to be told: without this,
// every save leaked a PDH query for the lifetime of the process.
func TestStoppingClosesSourcesThatOwnSomething(t *testing.T) {
	closing := &closingSource{}
	s := &sensors{sources: []collector.Source{closing, plainSource{}}}

	s.stop()

	if closing.closed != 1 {
		t.Errorf("Close called %d times, want once", closing.closed)
	}
}
