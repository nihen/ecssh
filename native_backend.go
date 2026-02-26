package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/mmmorris1975/ssm-session-client/datachannel"
	"golang.org/x/term"
)

// NativeBackend connects to ECS containers using the ssm-session-client
// library directly, without requiring the external session-manager-plugin binary.
type NativeBackend struct {
	awsCfg aws.Config
}

// Connect establishes a session to an ECS container via the SSM data channel.
// It uses Open() (via a mock SSM endpoint returning the pre-existing session)
// to properly initialize internal message buffers and the outbound queue processor,
// which are required for reliable bidirectional communication.
func (b *NativeBackend) Connect(streamUrl, tokenValue string, verbose bool) error {
	// Suppress internal library log output (ssm-session-client uses log.Printf)
	if !verbose {
		prevWriter := log.Writer()
		prevFlags := log.Flags()
		prevPrefix := log.Prefix()
		log.SetOutput(io.Discard)
		defer func() {
			log.SetOutput(prevWriter)
			log.SetFlags(prevFlags)
			log.SetPrefix(prevPrefix)
		}()
	}

	c, err := openDataChannelWithSession(b.awsCfg, streamUrl, tokenValue)
	if err != nil {
		return err
	}
	defer c.Close()

	fd := int(os.Stdin.Fd())
	var restoreOnce sync.Once

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if term.IsTerminal(fd) {
		oldState, err := term.MakeRaw(fd)
		if err == nil {
			defer restoreOnce.Do(func() { term.Restore(fd, oldState) })

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			defer signal.Stop(sigCh)
			go func() {
				<-sigCh
				restoreOnce.Do(func() { term.Restore(fd, oldState) })
				cancel()
			}()
		}

		sendTerminalSize(c)
		go watchTerminalSize(ctx, c)
	}

	// stdin -> datachannel in background (will be abandoned when output finishes)
	go func() {
		io.Copy(c, os.Stdin)
	}()

	// datachannel -> stdout (blocks until session closes)
	_, err = io.Copy(os.Stdout, c)
	cancel()
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// openDataChannelWithSession creates a fully initialized SsmDataChannel using
// a pre-existing session (StreamUrl/TokenValue from ECS ExecuteCommand).
// It routes the SSM StartSession API call through a local mock endpoint that
// returns the existing session data, allowing Open() to do its full initialization
// (message buffers, outbound queue processor) before connecting to the real websocket.
// The provided aws.Config is reused (from ECSClient) to avoid a second credential
// resolution, with only the SSM endpoint overridden to point at the mock server.
func openDataChannelWithSession(baseCfg aws.Config, streamUrl, tokenValue string) (*datachannel.SsmDataChannel, error) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := struct {
			SessionId  string `json:"SessionId"`
			StreamUrl  string `json:"StreamUrl"`
			TokenValue string `json:"TokenValue"`
		}{
			SessionId:  "ecssh-native",
			StreamUrl:  streamUrl,
			TokenValue: tokenValue,
		}
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	// Copy the base config and override only the SSM endpoint
	cfg := baseCfg.Copy()
	cfg.EndpointResolverWithOptions = aws.EndpointResolverWithOptionsFunc(
		func(service, region string, options ...interface{}) (aws.Endpoint, error) {
			if service == ssm.ServiceID {
				return aws.Endpoint{
					URL:          ts.URL,
					SigningRegion: region,
				}, nil
			}
			return aws.Endpoint{}, &aws.EndpointNotFoundError{}
		},
	)

	c := new(datachannel.SsmDataChannel)
	if err := c.Open(cfg, &ssm.StartSessionInput{
		Target: aws.String("ecssh-native"),
	}); err != nil {
		return nil, fmt.Errorf("failed to open session: %w", err)
	}

	return c, nil
}

func sendTerminalSize(c *datachannel.SsmDataChannel) {
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return
	}
	// term.GetSize returns (width, height) but SetTerminalSize expects (rows, cols)
	c.SetTerminalSize(uint32(height), uint32(width))
}
