package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

type ECSClient struct {
	client *ecs.Client
	region string
	ctx    context.Context
}

type TaskInfo struct {
	TaskArn        string
	TaskDefName    string
	ContainerNames []string
}



func NewECSClient() (*ECSClient, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return nil, err
	}

	return &ECSClient{
		client: ecs.NewFromConfig(cfg),
		region: cfg.Region,
		ctx:    context.TODO(),
	}, nil
}

func (e *ECSClient) ListClusters() ([]types.Cluster, error) {
	result, err := e.client.ListClusters(e.ctx, &ecs.ListClustersInput{})
	if err != nil {
		return nil, err
	}

	if len(result.ClusterArns) == 0 {
		return nil, nil
	}

	// Get detailed cluster information
	clusterDetails, err := e.client.DescribeClusters(e.ctx, &ecs.DescribeClustersInput{
		Clusters: result.ClusterArns,
	})
	if err != nil {
		return nil, err
	}

	return clusterDetails.Clusters, nil
}

func (e *ECSClient) ListTasksInCluster(clusterName string) ([]TaskInfo, error) {
	// Get running tasks
	taskList, err := e.client.ListTasks(e.ctx, &ecs.ListTasksInput{
		Cluster:       aws.String(clusterName),
		DesiredStatus: types.DesiredStatusRunning,
	})
	if err != nil {
		return nil, err
	}

	if len(taskList.TaskArns) == 0 {
		return nil, nil
	}

	// Get task details with containers - batch operation
	taskDetails, err := e.client.DescribeTasks(e.ctx, &ecs.DescribeTasksInput{
		Cluster: aws.String(clusterName),
		Tasks:   taskList.TaskArns,
	})
	if err != nil {
		return nil, err
	}

	var tasks []TaskInfo
	for _, task := range taskDetails.Tasks {
		taskDefName := extractTaskDefName(*task.TaskDefinitionArn)
		containerNames := extractRunningContainers(task.Containers)

		tasks = append(tasks, TaskInfo{
			TaskArn:        *task.TaskArn,
			TaskDefName:    taskDefName,
			ContainerNames: containerNames,
		})
	}

	return tasks, nil
}

func (e *ECSClient) FindMatchingTasks(clusterName, taskNamePattern string) ([]TaskInfo, error) {
	tasks, err := e.ListTasksInCluster(clusterName)
	if err != nil {
		return nil, err
	}

	var matching []TaskInfo
	for _, task := range tasks {
		if strings.Contains(task.TaskDefName, taskNamePattern) {
			matching = append(matching, task)
		}
	}

	return matching, nil
}


func extractTaskDefName(taskDefArn string) string {
	parts := strings.Split(taskDefArn, "/")
	if len(parts) > 0 {
		taskDefWithVersion := parts[len(parts)-1]
		// Remove version number
		taskDefParts := strings.Split(taskDefWithVersion, ":")
		if len(taskDefParts) > 0 {
			return taskDefParts[0]
		}
	}
	return taskDefArn
}

func extractRunningContainers(containers []types.Container) []string {
	var names []string
	for _, container := range containers {
		if container.LastStatus != nil && *container.LastStatus == "RUNNING" {
			names = append(names, *container.Name)
		}
	}
	return names
}

func extractTaskId(taskArn string) string {
	parts := strings.Split(taskArn, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return taskArn
}

// filteredWriter filters out unwanted messages from session-manager-plugin
type filteredWriter struct {
	writer io.Writer
	filter func(string) bool
}

func (f *filteredWriter) Write(p []byte) (n int, err error) {
	scanner := bufio.NewScanner(strings.NewReader(string(p)))
	for scanner.Scan() {
		line := scanner.Text()
		if f.filter(line) {
			f.writer.Write([]byte(line + "\n"))
		}
	}
	return len(p), nil
}

func (e *ECSClient) connectToContainer(clusterName, taskArn, containerName string, verbose bool) error {
	// Get session from ECS ExecuteCommand
	// Always use /bin/bash for container shell (ECS containers are typically Linux)
	execResult, err := e.client.ExecuteCommand(e.ctx, &ecs.ExecuteCommandInput{
		Cluster:     aws.String(clusterName),
		Task:        aws.String(taskArn),
		Container:   aws.String(containerName),
		Interactive: true,
		Command:     aws.String("/bin/bash"),
	})
	if err != nil {
		return fmt.Errorf("ExecuteCommand failed: %v", err)
	}

	// Marshal session data
	sessionData, err := json.Marshal(execResult.Session)
	if err != nil {
		return fmt.Errorf("failed to marshal session: %v", err)
	}

	// Get task details to extract runtime ID
	taskDetails, err := e.client.DescribeTasks(e.ctx, &ecs.DescribeTasksInput{
		Cluster: aws.String(clusterName),
		Tasks:   []string{taskArn},
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
		if *container.Name == containerName {
			runtimeId = *container.RuntimeId
			break
		}
	}

	if runtimeId == "" {
		return fmt.Errorf("runtime ID not found for container %s", containerName)
	}

	// Create SSM target
	target := &ssm.StartSessionInput{
		Target: aws.String(fmt.Sprintf("ecs:%s_%s_%s", clusterName, extractTaskId(taskArn), runtimeId)),
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
		string(sessionData),
		e.region,
		"StartSession",
		"", // profile (empty for default)
		string(targetData),
		fmt.Sprintf("https://ecs.%s.amazonaws.com", e.region))

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
		for scanner.Scan() {
			line := scanner.Text()
			// Filter out "Starting session" message
			if !strings.Contains(line, "Starting session with SessionId:") {
				fmt.Fprintln(os.Stderr, line)
			}
		}
	}()

	// Wait for command to complete
	err = cmd.Wait()
	wg.Wait()
	
	return err
}

func (e *ECSClient) tryConnectWithFallback(cluster, taskName string, force, verbose bool) error {
	// Find matching tasks
	matchingTasks, err := e.FindMatchingTasks(cluster, taskName)
	if err != nil {
		return fmt.Errorf("error finding tasks: %v", err)
	}
	
	if len(matchingTasks) == 0 {
		return fmt.Errorf("no tasks matching '%s'", taskName)
	}
	
	// Use first matching task
	selectedTask := matchingTasks[0]
	if verbose {
		taskId := extractTaskId(selectedTask.TaskArn)
		fmt.Printf("Found matching task: %s\n", taskId)
		fmt.Printf("Found %d running container(s)\n", len(selectedTask.ContainerNames))
	}
	
	// Select container
	containerName, err := selectContainer(selectedTask.ContainerNames, force)
	if err != nil {
		return fmt.Errorf("error selecting container: %v", err)
	}
	
	if verbose {
		fmt.Printf("Selected container: %s\n", containerName)
	}
	
	// Connect
	return e.connectToContainer(cluster, selectedTask.TaskArn, containerName, verbose)
}

func selectFromList(prompt string, items []string) (int, error) {
	if len(items) == 0 {
		return -1, fmt.Errorf("no items available")
	}

	fmt.Println(prompt)
	for i, item := range items {
		fmt.Printf("%d) %s\n", i+1, item)
	}

	for {
		fmt.Print("> ")
		var input string
		if _, err := fmt.Scanln(&input); err != nil {
			continue
		}

		selection, err := strconv.Atoi(input)
		if err != nil || selection < 1 || selection > len(items) {
			fmt.Printf("Invalid selection (1-%d)\n", len(items))
			continue
		}

		return selection - 1, nil
	}
}

func selectContainer(containers []string, force bool) (string, error) {
	if len(containers) == 0 {
		return "", fmt.Errorf("no containers available")
	}

	if force || len(containers) == 1 {
		return containers[0], nil
	}

	idx, err := selectFromList("Select container:", containers)
	if err != nil {
		return "", err
	}
	return containers[idx], nil
}

func interactiveMode(ecsClient *ECSClient) error {
	// Get clusters
	clusters, err := ecsClient.ListClusters()
	if err != nil {
		return fmt.Errorf("failed to list clusters: %v", err)
	}

	if len(clusters) == 0 {
		return fmt.Errorf("no ECS clusters found")
	}

	// Select cluster
	var clusterNames []string
	for _, cluster := range clusters {
		name := *cluster.ClusterName
		if cluster.RunningTasksCount > 0 {
			name = fmt.Sprintf("%s (%d running tasks)", name, cluster.RunningTasksCount)
		} else {
			name = fmt.Sprintf("%s (no running tasks)", name)
		}
		clusterNames = append(clusterNames, name)
	}

	idx, err := selectFromList("Select cluster:", clusterNames)
	if err != nil {
		return err
	}
	selectedCluster := *clusters[idx].ClusterName

	// Get tasks in the selected cluster
	tasks, err := ecsClient.ListTasksInCluster(selectedCluster)
	if err != nil {
		return fmt.Errorf("failed to list tasks: %v", err)
	}

	if len(tasks) == 0 {
		return fmt.Errorf("no running tasks in cluster %s", selectedCluster)
	}

	// Group tasks by task definition name
	taskGroups := make(map[string][]TaskInfo)
	for _, task := range tasks {
		taskGroups[task.TaskDefName] = append(taskGroups[task.TaskDefName], task)
	}

	// Select task definition
	var taskDefNames []string
	for name, taskList := range taskGroups {
		taskDefNames = append(taskDefNames, fmt.Sprintf("%s (%d tasks)", name, len(taskList)))
	}

	idx, err = selectFromList("Select task definition:", taskDefNames)
	if err != nil {
		return err
	}

	// Extract the task definition name from the selection
	selectedTaskDef := taskDefNames[idx]
	for name := range taskGroups {
		if strings.HasPrefix(selectedTaskDef, name) {
			selectedTaskDef = name
			break
		}
	}

	// Select specific task if multiple
	selectedTasks := taskGroups[selectedTaskDef]
	var selectedTask TaskInfo

	if len(selectedTasks) == 1 {
		selectedTask = selectedTasks[0]
	} else {
		var taskDescriptions []string
		for _, task := range selectedTasks {
			taskId := extractTaskId(task.TaskArn)
			containerList := strings.Join(task.ContainerNames, ", ")
			taskDescriptions = append(taskDescriptions, fmt.Sprintf("%s - Containers: %s", taskId, containerList))
		}

		idx, err = selectFromList("Select task:", taskDescriptions)
		if err != nil {
			return err
		}
		selectedTask = selectedTasks[idx]
	}

	// Select container
	containerName, err := selectContainer(selectedTask.ContainerNames, false)
	if err != nil {
		return err
	}

	fmt.Printf("\nConnecting to %s in task %s...\n", containerName, extractTaskId(selectedTask.TaskArn))

	// Connect
	return ecsClient.connectToContainer(selectedCluster, selectedTask.TaskArn, containerName, false)
}

func printUsage() {
	fmt.Print(`Usage: ecssh [SUBCOMMAND] [OPTIONS] [ARGUMENTS]

ECS Execute tool for connecting to ECS containers

SUBCOMMANDS:
  list             List all available ECS clusters (default)
  list clusters    List all available ECS clusters
  list tasks       List all running tasks in a cluster
  help             Show this help message

CONNECTION (no subcommand):
  ecssh                               # Interactive mode (select cluster/task/container)
  ecssh [OPTIONS] [CLUSTER_ID] [TASK_NAME]

Arguments:
  CLUSTER_ID     ECS cluster name or ARN
  TASK_NAME      Task definition name pattern

Environment variables:
  ECSSH_CLUSTER_ID    ECS cluster name or ARN
  ECSSH_TASK_NAME     Task definition name pattern to search for

Options:
  -f, --force    Connect to the first available container
  -v, --verbose  Show verbose output during execution

Examples:
  ecssh                               # Interactive mode
  ecssh help                          # Show this help
  ecssh list                          # List all clusters
  ecssh list clusters                 # List all clusters
  ecssh list tasks my-cluster         # List tasks in cluster
  ecssh my-cluster web-app            # Connect to container
  ecssh -f my-cluster web-app         # Force mode
`)
}

func main() {
	args := os.Args[1:]
	
	// If no arguments, start interactive mode
	if len(args) == 0 {
		// Try environment variables first
		cluster := os.Getenv("ECSSH_CLUSTER_ID")
		taskName := os.Getenv("ECSSH_TASK_NAME")
		if cluster != "" && taskName != "" {
			args = []string{cluster, taskName}
		} else {
			// Interactive mode
			ecsClient, err := NewECSClient()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error initializing AWS client: %v\n", err)
				os.Exit(1)
			}
			
			err = interactiveMode(ecsClient)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}
	}

	// Handle subcommands
	if args[0] == "help" {
		printUsage()
		return
	}

	ecsClient, err := NewECSClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing AWS client: %v\n", err)
		os.Exit(1)
	}

	if args[0] == "list" {
		// Default to "clusters" if no subcommand provided
		subcommand := "clusters"
		if len(args) >= 2 {
			subcommand = args[1]
		}

		switch subcommand {
		case "clusters":
			clusters, err := ecsClient.ListClusters()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error listing clusters: %v\n", err)
				os.Exit(1)
			}

			fmt.Println("Available clusters:")
			for _, cluster := range clusters {
				status := "UNKNOWN"
				if cluster.Status != nil {
					status = *cluster.Status
				}
				fmt.Printf("  - %s (status: %s, running tasks: %d)\n", 
					*cluster.ClusterName, status, cluster.RunningTasksCount)
				
				// Show tasks for this cluster if any exist
				if cluster.RunningTasksCount > 0 {
					fmt.Println("    Tasks:")
					tasks, err := ecsClient.ListTasksInCluster(*cluster.ClusterName)
					if err != nil {
						fmt.Printf("      Error listing tasks: %v\n", err)
					} else {
						for _, task := range tasks {
							taskId := extractTaskId(task.TaskArn)
							if len(taskId) > 12 {
								taskId = taskId[:12] + "..."
							}
							containerList := strings.Join(task.ContainerNames, ", ")
							if containerList == "" {
								containerList = "none"
							}
							fmt.Printf("      • %s (%s) - %s\n", taskId, task.TaskDefName, containerList)
						}
					}
				}
				fmt.Println()
			}
			return

		case "tasks":
			if len(args) < 3 {
				fmt.Fprintf(os.Stderr, "Error: Cluster ID required for listing tasks\n")
				os.Exit(1)
			}

			tasks, err := ecsClient.ListTasksInCluster(args[2])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error listing tasks: %v\n", err)
				os.Exit(1)
			}

			fmt.Printf("Running tasks in cluster: %s\n\n", args[2])
			for _, task := range tasks {
				taskId := extractTaskId(task.TaskArn)
				fmt.Printf("  - %s\n", taskId)
				fmt.Printf("    Definition: %s\n", task.TaskDefName)
				fmt.Printf("    Containers: %s\n\n", strings.Join(task.ContainerNames, ", "))
			}
			return
		}
	}

	// Parse connection arguments
	var force, verbose bool
	var cluster, taskName string
	var positionalArgs []string

	for _, arg := range args {
		switch arg {
		case "-f", "--force":
			force = true
		case "-v", "--verbose":
			verbose = true
		default:
			positionalArgs = append(positionalArgs, arg)
		}
	}

	if len(positionalArgs) >= 2 {
		cluster = positionalArgs[0]
		taskName = positionalArgs[1]
	} else {
		cluster = os.Getenv("ECSSH_CLUSTER_ID")
		taskName = os.Getenv("ECSSH_TASK_NAME")
		if len(positionalArgs) >= 1 {
			cluster = positionalArgs[0]
		}
	}

	if cluster == "" || taskName == "" {
		fmt.Fprintf(os.Stderr, "Error: Both CLUSTER_ID and TASK_NAME are required\n")
		printUsage()
		os.Exit(1)
	}

	if verbose {
		fmt.Printf("Searching for tasks in cluster: %s\n", cluster)
		fmt.Printf("Task name pattern: %s\n", taskName)
	}

	// Try connection with fallback
	err = ecsClient.tryConnectWithFallback(cluster, taskName, force, verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Connection failed: %v\n", err)
		os.Exit(1)
	}
}