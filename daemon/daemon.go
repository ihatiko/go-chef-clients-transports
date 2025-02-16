package daemon

import (
	"context"
	"errors"
	"github.com/ihatiko/go-chef-core-sdk/iface"
	"github.com/ihatiko/go-chef-core-sdk/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"log/slog"
	"sync"
	"time"
)

const (
	defaultTimeout  = 10 * time.Second
	defaultInterval = 10 * time.Second
	defaultWorker   = 1
	key             = "daemon"
)

// TODO exec metrics
var successDaemon = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "daemon_processing_success",
}, []string{})

var failedDaemon = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "daemon_processing_failed",
}, []string{})

type Request struct {
	ctx context.Context
	id  int
}

func (c *Request) Context() context.Context {
	return c.ctx
}

type h func(context Request) error

type Transport struct {
	types.Component
	Config *Config
	h      h
	Ticker *time.Ticker
}
type Options func(*Transport)

func (t Transport) GetKey() string {
	return key
}

type Details struct {
	Timeout  time.Duration `json:"timeout"`
	Interval time.Duration `json:"interval"`
	Workers  int           `json:"workers"`
}

func (t Transport) Details() any {
	return Details{
		Interval: t.Config.Interval,
		Timeout:  t.Config.Timeout,
		Workers:  t.Config.Workers,
	}
}
func (cfg *Config) Setup(fn h, opts ...Options) iface.IComponent {
	t := new(Transport)
	t.Config = cfg
	for _, opt := range opts {
		opt(t)
	}
	if t.Config.Timeout == 0 {
		t.Config.Timeout = defaultTimeout
	}
	if t.Config.Interval == 0 {
		t.Config.Interval = defaultInterval
	}
	t.Ticker = time.NewTicker(cfg.Interval)
	if t.Config.Workers != 0 {
		t.Config.Workers = defaultWorker
	}
	t.h = fn
	return t
}

func (t Transport) Routing(fn h) Transport {
	t.h = fn
	return t
}

func (t Transport) Run() error {
	slog.Info("starting daemon")
	if t.h == nil {
		return errors.New("daemon transport handler is nil")
	}
	for range t.Ticker.C {
		wg := &sync.WaitGroup{}
		wg.Add(t.Config.Workers)
		for i := range t.Config.Workers {
			go func(id int) {
				defer wg.Done()
				slog.Info("Start daemon worker",
					slog.Int("worker", i+1),
				)
				t.handler(i + 1)
				slog.Info("End daemon worker",
					slog.Int("worker", i+1),
				)
			}(i)
		}
		wg.Wait()
	}
	return nil
}

func (t Transport) handler(id int) {
	ctx, cancel := context.WithTimeout(context.TODO(), t.Config.Timeout)
	defer func() {
		if r := recover(); r != nil {
			slog.Error("recovering from panic", slog.Any("panic", r))
		}
	}()

	defer cancel()
	go func() {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				slog.Warn("context deadline exceeded daemon")
				return
			}
			if errors.Is(ctx.Err(), context.Canceled) {
				return
			}
			if ctx.Err() != nil {
				slog.Error("daemon error", slog.Any("error", ctx.Err()))
			}
		}
	}()
	err := t.h(Request{ctx: ctx, id: id})
	if err != nil {
		slog.Error("daemon error", slog.Any("error", err))
		failedDaemon.WithLabelValues().Inc()
		return
	}
	successDaemon.WithLabelValues().Inc()
}

func (t Transport) Live(ctx context.Context) error {
	return nil
}

func (t Transport) TimeToWait() time.Duration {
	return t.Config.Timeout
}

func (t Transport) Shutdown() error {
	t.Ticker.Stop()
	return nil
}
