// Package util provides helpers shared by the modules: request parsing and
// responses, database transactions and queries, context values and IDs.
package util

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/GoRustC/new-goadmin/pkg/logging"
)

// Run starts the service with handler and blocks until SIGINT, SIGTERM or
// SIGQUIT arrives, then calls the cleanup function handler returned and exits
// the process. SIGHUP is logged and ignored.
func Run(ctx context.Context, handler func(ctx context.Context) (func(), error)) error {
	state := 1
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	cleanFn, err := handler(ctx)
	if err != nil {
		return err
	}

EXIT:
	for {
		sig := <-sc
		logging.Context(ctx).Info("Received signal", zap.String("signal", sig.String()))

		switch sig {
		case syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT:
			state = 0
			break EXIT
		case syscall.SIGHUP:
		default:
			break EXIT
		}
	}

	cleanFn()
	logging.Context(ctx).Info("Server exit, bye...")
	time.Sleep(time.Millisecond * 100)
	os.Exit(state)
	return nil
}
