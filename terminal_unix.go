//go:build !windows

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/mmmorris1975/ssm-session-client/datachannel"
)

func watchTerminalSize(ctx context.Context, c *datachannel.SsmDataChannel) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH)
	defer signal.Stop(sigCh)

	for {
		select {
		case <-ctx.Done():
			return
		case <-sigCh:
			sendTerminalSize(c)
		}
	}
}
