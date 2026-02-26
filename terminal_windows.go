//go:build windows

package main

import (
	"context"
	"os"
	"time"

	"github.com/mmmorris1975/ssm-session-client/datachannel"
	"golang.org/x/term"
)

func watchTerminalSize(ctx context.Context, c *datachannel.SsmDataChannel) {
	lastWidth, lastHeight, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			width, height, err := term.GetSize(int(os.Stdout.Fd()))
			if err != nil {
				continue
			}
			if width != lastWidth || height != lastHeight {
				c.SetTerminalSize(uint32(height), uint32(width))
				lastWidth = width
				lastHeight = height
			}
		}
	}
}
