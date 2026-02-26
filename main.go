package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

type ECSClient struct {
	client *ecs.Client
	cfg    aws.Config
	region string
	ctx    context.Context
}

type TaskInfo struct {
	TaskArn        string
	TaskDefName    string
	ContainerNames []string
}

type Backend interface {
	Connect(streamUrl, tokenValue string, verbose bool) error
}

func NewECSClient() (*ECSClient, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return nil, err
	}

	return &ECSClient{
		client: ecs.NewFromConfig(cfg),
		cfg:    cfg,
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

func (e *ECSClient) connectToContainer(clusterName, taskArn, containerName, command, backendType string, verbose bool) error {
	// Get session from ECS ExecuteCommand
	command = strings.TrimSpace(command)
	if command == "" {
		command = "/bin/bash"
	}
	execResult, err := e.client.ExecuteCommand(e.ctx, &ecs.ExecuteCommandInput{
		Cluster:     aws.String(clusterName),
		Task:        aws.String(taskArn),
		Container:   aws.String(containerName),
		Interactive: true,
		Command:     aws.String(command),
	})
	if err != nil {
		return fmt.Errorf("ExecuteCommand failed: %v", err)
	}

	// Validate session response
	if execResult.Session == nil {
		return fmt.Errorf("ExecuteCommand returned nil session")
	}
	if execResult.Session.StreamUrl == nil {
		return fmt.Errorf("ExecuteCommand returned nil StreamUrl")
	}
	if execResult.Session.TokenValue == nil {
		return fmt.Errorf("ExecuteCommand returned nil TokenValue")
	}

	// Create the appropriate backend
	var backend Backend
	switch backendType {
	case "native":
		backend = &NativeBackend{awsCfg: e.cfg}
	case "plugin":
		sessionJSON, err := json.Marshal(execResult.Session)
		if err != nil {
			return fmt.Errorf("failed to marshal session: %v", err)
		}
		backend = &PluginBackend{
			ecsClient:     e,
			clusterName:   clusterName,
			taskArn:       taskArn,
			containerName: containerName,
			sessionJSON:   sessionJSON,
		}
	default:
		return fmt.Errorf("unknown backend type: %s (must be 'plugin' or 'native')", backendType)
	}

	return backend.Connect(*execResult.Session.StreamUrl, *execResult.Session.TokenValue, verbose)
}

func (e *ECSClient) tryConnectWithFallback(cluster, taskName, containerFilter, command, backendType string, force, verbose bool) error {
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
	containerName, err := selectContainer(selectedTask.ContainerNames, containerFilter, force)
	if err != nil {
		return fmt.Errorf("error selecting container: %v", err)
	}

	if verbose {
		fmt.Printf("Selected container: %s\n", containerName)
	}

	// Connect
	return e.connectToContainer(cluster, selectedTask.TaskArn, containerName, command, backendType, verbose)
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

func selectContainer(containers []string, filter string, force bool) (string, error) {
	if len(containers) == 0 {
		return "", fmt.Errorf("no containers available")
	}

	// Filter containers if filter is provided
	var filtered []string
	if filter != "" {
		for _, container := range containers {
			if strings.Contains(container, filter) {
				filtered = append(filtered, container)
			}
		}
		if len(filtered) == 0 {
			return "", fmt.Errorf("no containers matching filter '%s'", filter)
		}
		containers = filtered
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

func interactiveMode(ecsClient *ECSClient, backendType string) error {
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
	containerName, err := selectContainer(selectedTask.ContainerNames, "", false)
	if err != nil {
		return err
	}

	fmt.Printf("\nConnecting to %s in task %s...\n", containerName, extractTaskId(selectedTask.TaskArn))

	// Connect
	command := os.Getenv("ECSSH_COMMAND")
	return ecsClient.connectToContainer(selectedCluster, selectedTask.TaskArn, containerName, command, backendType, false)
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
  ecssh [OPTIONS] [CLUSTER_ID] [TASK_NAME] [CONTAINER_FILTER]

Arguments:
  CLUSTER_ID        ECS cluster name or ARN
  TASK_NAME         Task definition name pattern
  CONTAINER_FILTER  Container name filter (partial match)

Environment variables:
  ECSSH_CLUSTER_ID        ECS cluster name or ARN
  ECSSH_TASK_NAME         Task definition name pattern to search for
  ECSSH_CONTAINER_FILTER  Container name filter
  ECSSH_COMMAND           Command to execute in the container
  ECSSH_BACKEND           Backend to use for connection (plugin or native)

Options:
  -f, --force              Connect to the first available container
  -v, --verbose            Show verbose output during execution
  -c, --command COMMAND    Command to execute in the container (default: /bin/bash)
  -b, --backend BACKEND    Backend to use: native (default) or plugin

Examples:
  ecssh                                          # Interactive mode
  ecssh help                                     # Show this help
  ecssh list                                     # List all clusters
  ecssh list clusters                            # List all clusters
  ecssh list tasks my-cluster                    # List tasks in cluster
  ecssh my-cluster web-app                       # Connect to container
  ecssh my-cluster web-app sidekiq               # Connect to sidekiq container
  ecssh -f my-cluster web-app                    # Force mode
  ecssh -c "ls -la" my-cluster web-app           # Run command and exit
  ecssh -c "/bin/sh" my-cluster web-app          # Use sh instead of bash
`)
}

func validateBackendType(backendType string) {
	switch backendType {
	case "plugin", "native":
	default:
		fmt.Fprintf(os.Stderr, "Error: invalid backend '%s' (must be 'plugin' or 'native')\n", backendType)
		os.Exit(1)
	}
}

func main() {
	args := os.Args[1:]

	// Determine backend type from environment (validated later, before connection)
	backendType := os.Getenv("ECSSH_BACKEND")
	if backendType == "" {
		backendType = "native"
	}

	// If no arguments, start interactive mode
	if len(args) == 0 {
		// Try environment variables first
		cluster := os.Getenv("ECSSH_CLUSTER_ID")
		taskName := os.Getenv("ECSSH_TASK_NAME")
		if cluster != "" && taskName != "" {
			args = []string{cluster, taskName}
		} else {
			// Interactive mode
			validateBackendType(backendType)
			ecsClient, err := NewECSClient()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error initializing AWS client: %v\n", err)
				os.Exit(1)
			}

			err = interactiveMode(ecsClient, backendType)
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
	var cluster, taskName, containerFilter, command string
	var positionalArgs []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-f", "--force":
			force = true
		case "-v", "--verbose":
			verbose = true
		case "-c", "--command":
			if i+1 < len(args) {
				i++
				command = args[i]
			} else {
				fmt.Fprintf(os.Stderr, "Error: -c/--command requires an argument\n")
				os.Exit(1)
			}
		case "-b", "--backend":
			if i+1 < len(args) {
				i++
				backendType = args[i]
				validateBackendType(backendType)
			} else {
				fmt.Fprintf(os.Stderr, "Error: -b/--backend requires an argument\n")
				os.Exit(1)
			}
		default:
			positionalArgs = append(positionalArgs, args[i])
		}
	}

	if len(positionalArgs) >= 3 {
		cluster = positionalArgs[0]
		taskName = positionalArgs[1]
		containerFilter = positionalArgs[2]
	} else if len(positionalArgs) >= 2 {
		cluster = positionalArgs[0]
		taskName = positionalArgs[1]
		containerFilter = os.Getenv("ECSSH_CONTAINER_FILTER")
	} else {
		cluster = os.Getenv("ECSSH_CLUSTER_ID")
		taskName = os.Getenv("ECSSH_TASK_NAME")
		containerFilter = os.Getenv("ECSSH_CONTAINER_FILTER")
		if len(positionalArgs) >= 1 {
			cluster = positionalArgs[0]
		}
	}

	if command == "" {
		command = os.Getenv("ECSSH_COMMAND")
	}

	if cluster == "" || taskName == "" {
		fmt.Fprintf(os.Stderr, "Error: Both CLUSTER_ID and TASK_NAME are required\n")
		printUsage()
		os.Exit(1)
	}

	if verbose {
		fmt.Printf("Searching for tasks in cluster: %s\n", cluster)
		fmt.Printf("Task name pattern: %s\n", taskName)
		if containerFilter != "" {
			fmt.Printf("Container filter: %s\n", containerFilter)
		}
		if command != "" {
			fmt.Printf("Command: %s\n", command)
		}
		fmt.Printf("Backend: %s\n", backendType)
	}

	// Validate backend before attempting connection
	validateBackendType(backendType)

	// Try connection with fallback
	err = ecsClient.tryConnectWithFallback(cluster, taskName, containerFilter, command, backendType, force, verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Connection failed: %v\n", err)
		os.Exit(1)
	}
}