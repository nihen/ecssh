# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`ecssh` is a Bash script tool for connecting to Amazon ECS (Elastic Container Service) containers using AWS ECS Execute Command. It provides an interactive interface for selecting and connecting to running containers in ECS clusters.

## Key Commands

### Running the Tool
```bash
# Subcommands
./ecssh help                      # Show help
./ecssh list clusters             # List all ECS clusters
./ecssh list tasks my-cluster     # List tasks in specific cluster

# Connection (no subcommand)
./ecssh my-cluster web-app        # Connect using command line arguments
./ecssh -f my-cluster web-app     # Force mode (skip interactive selection)
./ecssh -v my-cluster web-app     # Verbose mode

# Using environment variables
export ECSSH_CLUSTER_ID=my-cluster
export ECSSH_TASK_NAME=web-app
./ecssh                           # Connect using environment variables
./ecssh -f                        # Environment variables + force mode
```

### Development Commands
```bash
# Make the script executable
chmod +x ecssh

# Check script syntax
bash -n ecssh

# Run shellcheck for linting (if installed)
shellcheck ecssh
```

## Architecture Notes

### Subcommand Structure
- **help**: Display help message
- **list clusters**: List all ECS clusters
- **list tasks**: List tasks in specific cluster
- **Connection mode**: Direct connection without subcommand

### Script Structure
- **Caching System**: Uses `/tmp/ecssh-cache-$$` directory with 5-minute TTL for performance optimization
- **Error Handling**: Uses `set -euo pipefail` for strict error handling
- **Cleanup**: Implements trap for cache cleanup on exit
- **AWS Integration**: Requires AWS CLI to be installed and configured with appropriate ECS permissions

### Key Functions
- `cache_get()`, `cache_set()`, `cache_clear()`: Fast caching system for AWS API responses
- `list_clusters()`: Lists all ECS clusters with running task counts
- `list_tasks()`: Shows detailed task information for a specific cluster
- `usage()`: Displays help information

### Task Selection Flow
1. Retrieves running tasks from specified ECS cluster
2. Filters tasks by task definition name pattern
3. Lists containers in matching tasks
4. Allows interactive selection or uses first container in force mode
5. Executes `aws ecs execute-command` to establish connection

### Required AWS Permissions
The script requires the following AWS permissions:
- `ecs:ListClusters`
- `ecs:DescribeClusters`
- `ecs:ListTasks`
- `ecs:DescribeTasks`
- `ecs:ExecuteCommand`
