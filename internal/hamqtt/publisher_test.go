package hamqtt

import (
	"errors"
	"net"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/corgan2222/rig-exporter/internal/applog"
	"github.com/corgan2222/rig-exporter/internal/collector"
	"github.com/corgan2222/rig-exporter/internal/config"
)

type finishedToken struct {
	err error
}

func (t finishedToken) Wait() bool                     { return true }
func (t finishedToken) WaitTimeout(time.Duration) bool { return true }
func (t finishedToken) Error() error                   { return t.err }
func (t finishedToken) Done() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}

type fakeMQTTClient struct {
	connectErrors []error
	connectCalls  int
	connected     bool
	publishes     int
	disconnects   int
}

func (c *fakeMQTTClient) IsConnected() bool      { return c.connected }
func (c *fakeMQTTClient) IsConnectionOpen() bool { return c.connected }
func (c *fakeMQTTClient) Connect() mqtt.Token {
	c.connectCalls++
	var err error
	if len(c.connectErrors) > 0 {
		err = c.connectErrors[0]
		c.connectErrors = c.connectErrors[1:]
	}
	return finishedToken{err: err}
}
func (c *fakeMQTTClient) Disconnect(uint) {
	c.connected = false
	c.disconnects++
}
func (c *fakeMQTTClient) Publish(string, byte, bool, interface{}) mqtt.Token {
	c.publishes++
	return finishedToken{}
}
func (*fakeMQTTClient) Subscribe(string, byte, mqtt.MessageHandler) mqtt.Token {
	return finishedToken{}
}
func (*fakeMQTTClient) SubscribeMultiple(map[string]byte, mqtt.MessageHandler) mqtt.Token {
	return finishedToken{}
}
func (*fakeMQTTClient) Unsubscribe(...string) mqtt.Token     { return finishedToken{} }
func (*fakeMQTTClient) AddRoute(string, mqtt.MessageHandler) {}
func (*fakeMQTTClient) OptionsReader() mqtt.ClientOptionsReader {
	return mqtt.NewOptionsReader(mqtt.NewClientOptions())
}

// The initial connection is retried in the background. A failed attempt still
// has to reach Status; otherwise the interface says "connecting" forever while
// the useful socket error never reaches the application.
func TestInitialConnectionFailureReachesStatus(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("close reserved port: %v", err)
	}

	cfg := config.Defaults()
	cfg.MQTTHost = "127.0.0.1"
	cfg.MQTTPort = port
	publisher := New(cfg, applog.Discard(), nil)
	connecting := publisher.Status().Detail

	if err := publisher.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(publisher.Stop)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && publisher.Status().Detail == connecting {
		time.Sleep(10 * time.Millisecond)
	}
	status := publisher.Status()
	if got := status.Detail; got == connecting {
		t.Fatalf("status stayed at %q after the broker refused the connection", got)
	}
	if !status.Failed || status.Healthy {
		t.Errorf("failed connection status = healthy:%v failed:%v", status.Healthy, status.Failed)
	}
}

func TestInitialConnectionIsRetried(t *testing.T) {
	publisher := New(config.Defaults(), applog.Discard(), nil)
	publisher.connectRetry = time.Millisecond
	client := &fakeMQTTClient{connectErrors: []error{errors.New("first attempt"), nil}}

	publisher.connect(client)

	if client.connectCalls != 2 {
		t.Errorf("Connect calls = %d, want 2", client.connectCalls)
	}
	if !publisher.Status().Failed {
		t.Error("the first failed attempt was not recorded")
	}
}

func TestLateOrStaleConnectionIsIgnored(t *testing.T) {
	t.Run("after stop", func(t *testing.T) {
		publisher := New(config.Defaults(), applog.Discard(), nil)
		late := &fakeMQTTClient{}
		publisher.client = late
		publisher.Stop()

		publisher.onConnect(late)

		if publisher.connected {
			t.Error("a connection callback made a stopped publisher active again")
		}
		if late.publishes != 0 {
			t.Error("a stopped publisher announced itself online")
		}
	})

	t.Run("from replaced client", func(t *testing.T) {
		publisher := New(config.Defaults(), applog.Discard(), nil)
		current := &fakeMQTTClient{}
		late := &fakeMQTTClient{}
		publisher.client = current
		t.Cleanup(publisher.Stop)

		publisher.onConnect(late)

		if publisher.connected {
			t.Error("a replaced client made the publisher active")
		}
		if late.publishes != 0 {
			t.Error("a replaced client announced itself online")
		}
	})
}

func TestSuccessfulExportClearsAPreviousFailure(t *testing.T) {
	publisher := New(config.Defaults(), applog.Discard(), nil)
	client := &fakeMQTTClient{connected: true}
	publisher.client = client
	publisher.connected = true
	publisher.recordError(errors.New("temporary publish failure"))

	if err := publisher.Export(collector.Snapshot{}); err != nil {
		t.Fatalf("Export: %v", err)
	}

	status := publisher.Status()
	if status.Failed || !status.Healthy {
		t.Errorf("status after successful export = healthy:%v failed:%v", status.Healthy, status.Failed)
	}
}
