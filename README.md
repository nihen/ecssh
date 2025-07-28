# ecssh

A CLI tool for easy access to ECS containers

![Version](https://img.shields.io/badge/version-v0.0.2-blue)

## Overview

`ecssh` is a Go-based CLI tool that provides quick SSH-like access to ECS containers using AWS ECS Execute Command. Simply specify a cluster name and task name pattern to connect to running containers.

## Prerequisites

- AWS CLI installed and configured
- AWS Session Manager Plugin installed
- Proper AWS credentials configured
- Go 1.21+ (for manual installation only)
- Required AWS permissions:
  - `ecs:ListClusters`
  - `ecs:DescribeClusters`
  - `ecs:ListTasks`
  - `ecs:DescribeTasks`
  - `ecs:ExecuteCommand`

## Installation

### Quick Install (Recommended)

```bash
# Install to current directory
curl -L https://raw.githubusercontent.com/nihen/ecssh/main/install.sh | bash

# Install to specific directory (requires sudo for system directories)
curl -L https://raw.githubusercontent.com/nihen/ecssh/main/install.sh | ECSSH_INSTALL_DIR=/usr/local/bin bash
```

### Manual Installation

```bash
# Clone the repository
git clone https://github.com/nihen/ecssh.git
cd ecssh

# Build binaries
./build.sh

# Make the universal launcher executable
chmod +x ecssh

# Copy to PATH (optional)
sudo cp ecssh /usr/local/bin/
```

## Usage

### Basic Connection

```bash
# Connect using cluster name and task name pattern
ecssh my-cluster web-app

# Connect using cluster, task name pattern, and container filter
ecssh my-cluster web-app sidekiq

# Connect using environment variables
export ECSSH_CLUSTER_ID=my-cluster
export ECSSH_TASK_NAME=web-app
export ECSSH_CONTAINER_FILTER=sidekiq
ecssh
```

### Subcommands

```bash
# Show help
ecssh help

# List all ECS clusters
ecssh list clusters

# List tasks in a specific cluster
ecssh list tasks my-cluster
```

### Options

- `-f, --force` - Automatically connect to first container when multiple exist
- `-v, --verbose` - Show detailed execution logs

### Examples

```bash
# Connect to production web service
ecssh production-cluster web-service

# Connect to staging API with force mode
ecssh -f staging-cluster api-service

# Connect to development worker with verbose mode
ecssh -v development-cluster worker

# Connect to specific container (sidekiq) in web service
ecssh production-cluster web-service sidekiq

# Filter containers using environment variable
export ECSSH_CONTAINER_FILTER=nginx
ecssh production-cluster web-service

# Check clusters before connecting
ecssh list clusters
ecssh list tasks production-cluster
ecssh production-cluster web-service
```

## How It Works

1. Retrieves running tasks from the specified cluster
2. Filters by task definition name (partial match)
3. Selects the first matching task
4. Lists containers in the task
5. Filters containers by name if container filter is provided (partial match)
6. Shows selection menu for multiple containers (auto-select with `-f`)
7. Connects using AWS ECS Execute Command

## Troubleshooting

### "No running tasks found" Error

- Verify cluster name is correct
- Ensure tasks are in RUNNING state
- Check task list with `ecssh list tasks CLUSTER_NAME`

### "No tasks matching 'TASK_NAME'" Error

- Verify task definition name pattern
- Remember it uses partial matching

### AWS CLI Errors

- Ensure AWS CLI is properly installed
- Verify AWS credentials: `aws sts get-caller-identity`
- Check required IAM permissions

### Session Manager Errors

- Ensure Session Manager Plugin is installed
- Verify Execute Command is enabled for ECS tasks

## Environment Variables

- `ECSSH_CLUSTER_ID` - Default ECS cluster name
- `ECSSH_TASK_NAME` - Default task name pattern
- `ECSSH_CONTAINER_FILTER` - Default container name filter (v0.0.2+)

## Supported Platforms

- macOS (Intel & Apple Silicon)
- Linux (x86_64 & ARM64)
- Windows (x86_64 & ARM64)

## License

MIT License
