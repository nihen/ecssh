# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v0.0.2] - 2025-07-28

### Added
- Container name filtering feature with new third argument
- Support for `ECSSH_CONTAINER_FILTER` environment variable
- Partial match filtering for container names to quickly select specific containers

### Changed
- Updated `selectContainer` function to support container name filtering
- Enhanced help message to include container filter documentation
- Improved examples in README to demonstrate container filtering

### Fixed
- None

## [v0.0.1] - 2025-07-28

### Added
- Initial release
- Interactive ECS container selection
- Support for cluster and task name pattern arguments
- Environment variable support (`ECSSH_CLUSTER_ID`, `ECSSH_TASK_NAME`)
- Force mode (`-f`) to skip interactive selection
- Verbose mode (`-v`) for detailed logging
- List subcommands for clusters and tasks
- Multi-platform support (macOS, Linux, Windows)
- Universal launcher script