package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

// PluginBackend connects to ECS containers by launching the
// session-manager-plugin external binary.
type PluginBackend struct {
	ecsClient     *ECSClient
	clusterName   string
	taskArn       string
	containerName string
	sessionJSON   []byte // marshaled session from ExecuteCommand
}

// Connect establishes an ECS Execute Command session via session-manager-plugin.
// streamUrl and tokenValue are accepted to satisfy the Backend interface but are
// not used directly; the pre-marshaled sessionJSON is passed to the plugin instead.
func (b *PluginBackend) Connect(streamUrl, tokenValue string, verbose bool) error {
	// Get task details to extract runtime ID
	taskDetails, err := b.ecsClient.client.DescribeTasks(b.ecsClient.ctx, &ecs.DescribeTasksInput{
		Cluster: aws.String(b.clusterName),
		Tasks:   []string{b.taskArn},
	})
	if err != nil {
		return fmt.Errorf("failed to describe task: %v", err)
	}

	if len(taskDetails.Tasks) == 0 {
		return fmt.Errorf("task not found")
	}

	// Find the runtime ID for the specified container
	var runtimeId string
	for _, container := range taskDetails.Tasks[0].Containers {
		if container.Name != nil && *container.Name == b.containerName {
			if container.RuntimeId != nil {
				runtimeId = *container.RuntimeId
			}
			break
		}
	}

	if runtimeId == "" {
		return fmt.Errorf("runtime ID not found for container %s", b.containerName)
	}

	// Create SSM target
	target := &ssm.StartSessionInput{
		Target: aws.String(fmt.Sprintf("ecs:%s_%s_%s", b.clusterName, extractTaskId(b.taskArn), runtimeId)),
	}

	targetData, err := json.Marshal(target)
	if err != nil {
		return fmt.Errorf("failed to marshal target: %v", err)
	}

	// Call session-manager-plugin directly
	// On Windows, the plugin might have .exe extension
	pluginName := "session-manager-plugin"
	if runtime.GOOS == "windows" {
		// Try to find the plugin with .exe extension if not in PATH
		if _, err := exec.LookPath(pluginName); err != nil {
			pluginName = "session-manager-plugin.exe"
		}
	}

	cmd := exec.Command(pluginName,
		string(b.sessionJSON),
		b.ecsClient.region,
		"StartSession",
		"", // profile (empty for default)
		string(targetData),
		fmt.Sprintf("https://ecs.%s.amazonaws.com", b.ecsClient.region))

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout

	// Filter stderr to remove unwanted messages
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %v", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start session-manager-plugin: %v", err)
	}

	// Copy stderr, filtering out unwanted messages
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderrPipe)
		scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			// Filter out "Starting session" message
			if !strings.Contains(line, "Starting session with SessionId:") {
				fmt.Fprintln(os.Stderr, line)
			}
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "error reading session-manager-plugin stderr: %v\n", err)
		}
	}()

	// Wait for command to complete
	err = cmd.Wait()
	wg.Wait()

	return err
}
