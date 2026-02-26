# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`ecssh` is a Go-based CLI tool for connecting to Amazon ECS (Elastic Container Service) containers using AWS ECS Execute Command. It provides an interactive interface for selecting and connecting to running containers in ECS clusters.

## Key Commands

### Running the Tool
```bash
# Subcommands
./ecssh help                      # Show help
./ecssh list clusters             # List all ECS clusters
./ecssh list tasks my-cluster     # List tasks in specific cluster

# Connection (no subcommand)
./ecssh my-cluster web-app        # Connect using command line arguments
./ecssh my-cluster web-app sidekiq # Connect with container filter
./ecssh -f my-cluster web-app     # Force mode (skip interactive selection)
./ecssh -v my-cluster web-app     # Verbose mode
./ecssh -c "ls -la" my-cluster web-app  # Run command in container
./ecssh -b plugin my-cluster web-app   # Use plugin backend (requires session-manager-plugin)
./ecssh --backend native my-cluster web-app  # Explicitly use native backend (default)

# Using environment variables
export ECSSH_CLUSTER_ID=my-cluster
export ECSSH_TASK_NAME=web-app
export ECSSH_CONTAINER_FILTER=sidekiq  # Optional container filter
export ECSSH_COMMAND="ls -la"          # Optional command (default: /bin/bash)
export ECSSH_BACKEND=native            # Optional backend selection (default: native)
./ecssh                           # Connect using environment variables
./ecssh -f                        # Environment variables + force mode
```

### Development Commands
```bash
# Build
go build -o /dev/null ./...

# Build for all platforms
./build.sh
```

## Architecture Notes

### Subcommand Structure
- **help**: Display help message
- **list clusters**: List all ECS clusters
- **list tasks**: List tasks in specific cluster
- **Connection mode**: Direct connection without subcommand

### Application Structure
- **Backend System**: Supports pluggable backends via the Backend interface (`NativeBackend` and `PluginBackend`)
  - **NativeBackend**: Uses `ssm-session-client` library for direct SSM sessions without requiring session-manager-plugin
  - **PluginBackend**: Delegates to `session-manager-plugin` binary (legacy, requires external plugin)
- **Terminal Management**: Handles terminal size tracking with SIGWINCH support (Unix) and polling (Windows), with graceful restore on signal interruption
- **AWS Integration**: Uses AWS SDK for Go; requires proper AWS credentials configured with appropriate ECS permissions

### Key Functions
- `NewECSClient()`: Creates AWS ECS client with default configuration
- `ListClusters()`: Lists all ECS clusters with running task counts
- `ListTasksInCluster()`: Shows detailed task information for a specific cluster
- `FindMatchingTasks()`: Filters tasks by name pattern
- `selectContainer()`: Selects container with optional name filter
- `connectToContainer()`: Establishes ECS Execute Command session

### Task Selection Flow
1. Retrieves running tasks from specified ECS cluster
2. Filters tasks by task definition name pattern
3. Lists containers in matching tasks
4. Filters containers by name if container filter is provided (v0.0.2+)
5. Allows interactive selection or uses first container in force mode
6. Executes `aws ecs execute-command` to establish connection

### Required AWS Permissions
The script requires the following AWS permissions:
- `ecs:ListClusters`
- `ecs:DescribeClusters`
- `ecs:ListTasks`
- `ecs:DescribeTasks`
- `ecs:ExecuteCommand`
