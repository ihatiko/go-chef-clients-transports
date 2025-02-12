package cron

import (
	"context"
	"errors"
	"fmt"
	"github.com/ihatiko/go-chef-core-sdk/iface"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	defaultTimeout = 10 * time.Second
	defaultWorker  = 1
	componentName  = "cron"
)

type Request struct {
	ctx context.Context
	id  int
}

func (c *Request) Context() context.Context {
	return c.ctx
}

type h func(context Request) error

type Transport struct {
	Config *Config
	h      h
}

func (t Transport) Name() string {
	return fmt.Sprintf("%s id: %s", componentName, uuid.New().String())
}

type Options func(*Transport)

func (cfg *Config) Setup(fn h, opts ...Options) iface.IComponent {
	t := new(Transport)

	t.Config = cfg
	for _, opt := range opts {
		opt(t)
	}
	if t.Config.Timeout == 0 {
		t.Config.Timeout = defaultTimeout
	}
	if t.Config.Workers != 0 {
		t.Config.Workers = defaultWorker
	}
	t.h = fn
	return t
}

func (t Transport) Routing(h h) Transport {
	t.h = h
	return t
}
func (t Transport) Live(ctx context.Context) error {
	return nil
}
func (t Transport) Run() error {
	if t.h == nil {
		return errors.New("cron transport handler is nil")
	}
	wg := &sync.WaitGroup{}
	wg.Add(t.Config.Workers)
	for i := range t.Config.Workers {
		go func(id int) {
			defer wg.Done()
			slog.Info("Start cron worker",
				slog.Int("worker", i+1),
			)
			t.handler(id + 1)
			slog.Info("End cron worker",
				slog.Int("worker", i+1),
			)
		}(i)
	}
	wg.Wait()
	return nil
}

func (t Transport) handler(id int) {
	ctx, cancel := context.WithTimeout(context.TODO(), t.Config.Timeout)
	defer func() {
		if r := recover(); r != nil {
			slog.Error("error handling message", slog.Any("panic", r))
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
				slog.Error("error handling context", slog.Any("error", ctx.Err()))
			}
		}
	}()
	err := t.h(Request{ctx: ctx, id: id})
	if err != nil {
		slog.Error("Error daemon worker", slog.Any("error", err))
	}
}

func (t Transport) TimeToWait() time.Duration {
	return t.Config.Timeout
}

func (t Transport) Shutdown() error {
	return nil
}
