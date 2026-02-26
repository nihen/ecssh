# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v0.0.4] - 2026-02-27

### Added
- Native SSM session backend (`--backend native` or `-b native`) using `ssm-session-client` library
- Backend selection option (`-b`/`--backend`) with `native` (default) and `plugin` backends
- Support for `ECSSH_BACKEND` environment variable
- Terminal size tracking with SIGWINCH support (Unix) and polling (Windows)
- Graceful terminal restore on signal interruption

### Changed
- Refactored connection logic into Backend interface pattern (NativeBackend, PluginBackend)
- Session Manager Plugin is now optional (only required for `--backend plugin`)

## [v0.0.3] - 2026-02-26

### Added
- Custom command option (`-c`/`--command`) to specify the command executed in the container
- Support for `ECSSH_COMMAND` environment variable
- Default command remains `/bin/bash` when not specified

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