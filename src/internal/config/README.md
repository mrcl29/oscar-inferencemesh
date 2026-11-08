# EPP Configuration (`src/internal/config`)

This package loads environment-backed runtime configuration for the Endpoint Picker (EPP) and OSCAR control plane connector.

## Files

| File                                                                                                                      | Type      | Description                                                                                                                                                   |
| :------------------------------------------------------------------------------------------------------------------------ | :-------- | :------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| [`settings.go`](file:///home/marc/Projects/oscar-inferencemesh/src/internal/config/settings.go)                           | Go Source | Defines immutable runtime `Settings` with 4-tier precedence (Flags > Env > Persistent File > Defaults).                                                       |
| [`file_config.go`](file:///home/marc/Projects/oscar-inferencemesh/src/internal/config/file_config.go)                     | Go Source | Persistent YAML configuration manager (`~/.config/oscar-im/config.yaml`) with XDG support and automatic `~/.oscar-cli/config.yaml` cluster import.            |
| [`interactive_menu.go`](file:///home/marc/Projects/oscar-inferencemesh/src/internal/config/interactive_menu.go)           | Go Source | Full interactive Terminal UI (TUI) powered by Charm (`bubbletea`, `lipgloss`, `bubbles`) with arrow navigation, alternate screen buffer, and CLI subcommands. |
| [`file_config_test.go`](file:///home/marc/Projects/oscar-inferencemesh/src/internal/config/file_config_test.go)           | Go Test   | Unit tests verifying creation, serialization, cluster switching, and `oscar-cli` import.                                                                      |
| [`interactive_menu_test.go`](file:///home/marc/Projects/oscar-inferencemesh/src/internal/config/interactive_menu_test.go) | Go Test   | Unit tests verifying subcommands (`view`, `path`, `set`, `use`, `help`) and Bubble Tea key event navigation.                                                  |
