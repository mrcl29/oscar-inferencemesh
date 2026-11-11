// Package config provides the interactive console configuration menu and CLI handlers.
package config

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
	"gopkg.in/yaml.v3"
)

// Styling definitions powered by Lip Gloss
var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#6366F1")).
			Padding(0, 2).
			MarginBottom(1)

	borderBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#6366F1")).
			Padding(0, 1).
			MarginBottom(1)

	metaLabelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#818CF8"))

	metaValueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E2E8F0"))

	activeCursorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#10B981"))

	selectedItemStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#38BDF8"))

	normalItemStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#94A3B8"))

	successNoticeStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#34D399"))

	errorNoticeStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#F87171"))

	warnNoticeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FBBF24"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#64748B")).
			MarginTop(1)
)

// RunConfigCommand handles the `oscar-im config` command line arguments.
// When invoked without arguments, it launches the full interactive terminal menu.
func RunConfigCommand(args []string, in io.Reader, out io.Writer) error {
	if len(args) > 0 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
		printConfigHelp(out)
		return nil
	}

	path := GetConfigFilePath()
	cfg, err := LoadOrCreateFileConfig(path)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	if len(args) == 0 {
		return RunConfigInteractive(cfg, path, in, out)
	}

	subcommand := args[0]
	switch subcommand {
	case "view", "show", "cat":
		return PrintConfig(cfg, out)

	case "path":
		fmt.Fprintln(out, path)
		return nil

	case "use", "use-cluster":
		if len(args) < 2 {
			return fmt.Errorf("usage: oscar-im config use <cluster-name>")
		}
		clusterName := args[1]
		if _, ok := cfg.Clusters[clusterName]; !ok {
			return fmt.Errorf("cluster '%s' does not exist in configuration", clusterName)
		}
		cfg.CurrentCluster = clusterName
		if err := SaveFileConfig(path, cfg); err != nil {
			return err
		}
		fmt.Fprintf(out, "Switched active cluster to '%s'\n", clusterName)
		return nil

	case "import", "import-oscar":
		oscarPath := GetOscarCLIConfigPath()
		if len(args) > 1 {
			oscarPath = args[1]
		}
		if err := cfg.ImportFromOscarCLI(oscarPath); err != nil {
			return fmt.Errorf("failed to import clusters from %s: %w", oscarPath, err)
		}
		if err := SaveFileConfig(path, cfg); err != nil {
			return err
		}
		fmt.Fprintf(out, "Successfully imported cluster definitions from %s\n", oscarPath)
		return nil

	case "set":
		if len(args) < 3 {
			return fmt.Errorf("usage: oscar-im config set <key> <value>")
		}
		key, val := args[1], args[2]
		return setConfigKey(cfg, path, key, val, out)

	default:
		return fmt.Errorf("unknown config subcommand '%s'. Run 'oscar-im config --help'", subcommand)
	}
}

func printConfigHelp(out io.Writer) {
	fmt.Fprintln(out, "Usage: oscar-im config [subcommand]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "When run with no arguments, launches the interactive configuration menu.")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Subcommands:")
	fmt.Fprintln(out, "  view, show            Display current configuration YAML")
	fmt.Fprintln(out, "  path                  Show path to active configuration file")
	fmt.Fprintln(out, "  use <cluster-name>    Switch the active OSCAR cluster")
	fmt.Fprintln(out, "  import [path]         Import clusters from ~/.oscar-cli/config.yaml")
	fmt.Fprintln(out, "  set <key> <value>     Update a default property (model, namespace, http_port, grpc_port, worker_wait_timeout_sec, docker)")
}

// PrintConfig outputs the configuration in formatted YAML.
func PrintConfig(cfg *FileConfig, out io.Writer) error {
	encoded, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, string(encoded))
	return nil
}

func setConfigKey(cfg *FileConfig, path, key, val string, out io.Writer) error {
	switch strings.ToLower(key) {
	case "model":
		cfg.Defaults.Model = val
	case "namespace":
		cfg.Defaults.Namespace = val
	case "http_port":
		p, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("invalid port: %s", val)
		}
		cfg.Defaults.HTTPPort = p
	case "grpc_port":
		p, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("invalid port: %s", val)
		}
		cfg.Defaults.GRPCPort = p
	case "worker_wait_timeout_sec":
		p, err := strconv.Atoi(val)
		if err != nil || p <= 0 {
			return fmt.Errorf("invalid worker wait timeout: %s (must be greater than zero)", val)
		}
		cfg.Defaults.WorkerWaitTimeout = p
	case "docker":
		cfg.Defaults.Docker = val == "true" || val == "1"
	default:
		return fmt.Errorf("unsupported config key '%s'. Supported keys: model, namespace, http_port, grpc_port, worker_wait_timeout_sec, docker", key)
	}

	if err := SaveFileConfig(path, cfg); err != nil {
		return err
	}
	fmt.Fprintf(out, "Updated %s = %s\n", key, val)
	return nil
}

// RunConfigInteractive runs the interactive Bubble Tea menu.
// If input is non-interactive (pipe/headless), falls back gracefully.
func RunConfigInteractive(cfg *FileConfig, path string, in io.Reader, out io.Writer) error {
	// Detect if running in a real interactive terminal
	if f, ok := in.(*os.File); ok && !isatty.IsTerminal(f.Fd()) && !isatty.IsCygwinTerminal(f.Fd()) {
		return runHeadlessFallback(cfg, path, in, out)
	}

	model := newMenuModel(cfg, path)
	prog := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithInput(in),
		tea.WithOutput(out),
	)

	_, err := prog.Run()
	return err
}

// Fallback for non-interactive pipelines
func runHeadlessFallback(cfg *FileConfig, path string, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	fmt.Fprintln(out, "OSCAR-IM Configuration Manager (non-interactive mode)")
	fmt.Fprintf(out, "Active cluster: %s\n", cfg.CurrentCluster)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "view" {
			_ = PrintConfig(cfg, out)
		} else if line == "save" {
			_ = SaveFileConfig(path, cfg)
		} else if line == "exit" || line == "quit" {
			break
		}
	}
	return nil
}

// --- Bubble Tea State Machine ---

type screenState int

const (
	stateMainMenu screenState = iota
	stateSwitchCluster
	stateClusterListForEdit
	stateClusterForm
	stateDeleteClusterList
	stateConfirmDeleteCluster
	stateEditDefaults
	stateEditDefaultField
	stateShowYAML
	stateConfirmExit
)

type defaultFieldKind int

const (
	fieldModel defaultFieldKind = iota
	fieldNamespace
	fieldHTTPPort
	fieldGRPCPort
)

type menuModel struct {
	cfg        *FileConfig
	path       string
	state      screenState
	dirty      bool
	notice     string
	noticeType string // "success", "error", "warn"

	// Main menu state
	mainMenuItems []string
	mainCursor    int

	// Cluster selection & management
	clusterList       []string
	clusterListCursor int
	targetClusterName string

	// Cluster edit form
	formInputs     []textinput.Model
	formSSLVerify  bool
	formFocusIndex int

	// Defaults editor
	defaultsItems   []string
	defaultsCursor  int
	activeField     defaultFieldKind
	fieldInput      textinput.Model
	confirmExitList []string
	confirmExitIdx  int

	width  int
	height int
}

func newMenuModel(cfg *FileConfig, path string) *menuModel {
	mainItems := []string{
		"Switch active OSCAR cluster",
		"Add or edit an OSCAR cluster",
		"Delete an OSCAR cluster",
		"Edit runtime defaults (model, namespace, ports, docker)",
		"Import clusters from ~/.oscar-cli/config.yaml",
		"View complete configuration (YAML)",
		"Save configuration to disk",
		"Exit",
	}

	exitItems := []string{
		"Save changes and exit",
		"Discard changes and exit",
		"Cancel (return to menu)",
	}

	return &menuModel{
		cfg:             cfg,
		path:            path,
		state:           stateMainMenu,
		mainMenuItems:   mainItems,
		mainCursor:      0,
		confirmExitList: exitItems,
	}
}

func (m *menuModel) Init() tea.Cmd {
	return nil
}

func (m *menuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		}

		switch m.state {
		case stateMainMenu:
			return m.updateMainMenu(msg)
		case stateSwitchCluster:
			return m.updateSwitchCluster(msg)
		case stateClusterListForEdit:
			return m.updateClusterListForEdit(msg)
		case stateClusterForm:
			return m.updateClusterForm(msg)
		case stateDeleteClusterList:
			return m.updateDeleteClusterList(msg)
		case stateConfirmDeleteCluster:
			return m.updateConfirmDeleteCluster(msg)
		case stateEditDefaults:
			return m.updateEditDefaults(msg)
		case stateEditDefaultField:
			return m.updateEditDefaultField(msg)
		case stateShowYAML:
			return m.updateShowYAML(msg)
		case stateConfirmExit:
			return m.updateConfirmExit(msg)
		}
	}

	return m, nil
}

func (m *menuModel) updateMainMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.mainCursor > 0 {
			m.mainCursor--
		} else {
			m.mainCursor = len(m.mainMenuItems) - 1
		}
	case "down", "j":
		if m.mainCursor < len(m.mainMenuItems)-1 {
			m.mainCursor++
		} else {
			m.mainCursor = 0
		}
	case "s":
		if err := SaveFileConfig(m.path, m.cfg); err != nil {
			m.notice = fmt.Sprintf("Failed to save: %v", err)
			m.noticeType = "error"
		} else {
			m.notice = fmt.Sprintf("Configuration saved to %s", m.path)
			m.noticeType = "success"
			m.dirty = false
		}
	case "enter":
		m.notice = ""
		switch m.mainCursor {
		case 0: // Switch active cluster
			m.clusterList = m.getSortedClusterNames()
			m.clusterListCursor = 0
			m.state = stateSwitchCluster
		case 1: // Add or edit cluster
			m.clusterList = m.getSortedClusterNames()
			m.clusterListCursor = 0
			m.state = stateClusterListForEdit
		case 2: // Delete cluster
			m.clusterList = m.getSortedClusterNames()
			m.clusterListCursor = 0
			m.state = stateDeleteClusterList
		case 3: // Edit defaults
			m.refreshDefaultsItems()
			m.defaultsCursor = 0
			m.state = stateEditDefaults
		case 4: // Import clusters
			oscarPath := GetOscarCLIConfigPath()
			if err := m.cfg.ImportFromOscarCLI(oscarPath); err != nil {
				m.notice = fmt.Sprintf("Import error: %v", err)
				m.noticeType = "error"
			} else {
				m.notice = fmt.Sprintf("Successfully imported clusters from %s", oscarPath)
				m.noticeType = "success"
				m.dirty = true
			}
		case 5: // View complete YAML
			m.state = stateShowYAML
		case 6: // Save
			if err := SaveFileConfig(m.path, m.cfg); err != nil {
				m.notice = fmt.Sprintf("Failed to save: %v", err)
				m.noticeType = "error"
			} else {
				m.notice = fmt.Sprintf("Configuration successfully saved to %s", m.path)
				m.noticeType = "success"
				m.dirty = false
			}
		case 7: // Exit
			if m.dirty {
				m.confirmExitIdx = 0
				m.state = stateConfirmExit
			} else {
				return m, tea.Quit
			}
		}

	case "q", "esc":
		if m.dirty {
			m.confirmExitIdx = 0
			m.state = stateConfirmExit
		} else {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *menuModel) updateSwitchCluster(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	total := len(m.clusterList) + 1 // clusters + [Back]
	switch msg.String() {
	case "up", "k":
		if m.clusterListCursor > 0 {
			m.clusterListCursor--
		} else {
			m.clusterListCursor = total - 1
		}
	case "down", "j":
		if m.clusterListCursor < total-1 {
			m.clusterListCursor++
		} else {
			m.clusterListCursor = 0
		}
	case "enter":
		if m.clusterListCursor == len(m.clusterList) {
			// Back
			m.state = stateMainMenu
			return m, nil
		}
		selected := m.clusterList[m.clusterListCursor]
		m.cfg.CurrentCluster = selected
		m.dirty = true
		m.notice = fmt.Sprintf("Switched active cluster to '%s'", selected)
		m.noticeType = "success"
		m.state = stateMainMenu
	case "esc", "q":
		m.state = stateMainMenu
	}
	return m, nil
}

func (m *menuModel) updateClusterListForEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	total := len(m.clusterList) + 2 // clusters + [+ Add New Cluster] + [Back]
	switch msg.String() {
	case "up", "k":
		if m.clusterListCursor > 0 {
			m.clusterListCursor--
		} else {
			m.clusterListCursor = total - 1
		}
	case "down", "j":
		if m.clusterListCursor < total-1 {
			m.clusterListCursor++
		} else {
			m.clusterListCursor = 0
		}
	case "enter":
		if m.clusterListCursor == total-1 {
			// Back
			m.state = stateMainMenu
			return m, nil
		}
		if m.clusterListCursor == len(m.clusterList) {
			// Add new cluster
			m.initClusterForm("", ClusterConfig{Endpoint: "https://localhost.direct", SSLVerify: false})
			m.state = stateClusterForm
			return m, nil
		}
		// Edit selected cluster
		clusterName := m.clusterList[m.clusterListCursor]
		m.initClusterForm(clusterName, m.cfg.Clusters[clusterName])
		m.state = stateClusterForm
	case "esc", "q":
		m.state = stateMainMenu
	}
	return m, nil
}

func (m *menuModel) initClusterForm(name string, cl ClusterConfig) {
	m.targetClusterName = name
	m.formFocusIndex = 0
	m.formSSLVerify = cl.SSLVerify

	inputs := make([]textinput.Model, 4)
	inputs[0] = textinput.New()
	inputs[0].Placeholder = "local (cluster identifier)"
	inputs[0].SetValue(name)
	inputs[0].Focus()

	inputs[1] = textinput.New()
	inputs[1].Placeholder = "https://localhost.direct"
	inputs[1].SetValue(cl.Endpoint)

	inputs[2] = textinput.New()
	inputs[2].Placeholder = "admin"
	inputs[2].SetValue(cl.AuthUser)

	inputs[3] = textinput.New()
	inputs[3].Placeholder = "password/token"
	inputs[3].EchoMode = textinput.EchoPassword
	inputs[3].EchoCharacter = '•'
	inputs[3].SetValue(cl.AuthPassword)

	m.formInputs = inputs
}

func (m *menuModel) updateClusterForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	totalFormElements := 7 // 4 inputs + SSL toggle + Save + Cancel
	switch msg.String() {
	case "tab", "down":
		m.formFocusIndex = (m.formFocusIndex + 1) % totalFormElements
		m.updateFormFocus()
		return m, nil
	case "shift+tab", "up":
		m.formFocusIndex = (m.formFocusIndex - 1 + totalFormElements) % totalFormElements
		m.updateFormFocus()
		return m, nil
	case "ctrl+s":
		return m.saveClusterForm()
	case "enter":
		if m.formFocusIndex == 4 { // SSL Toggle
			m.formSSLVerify = !m.formSSLVerify
			return m, nil
		}
		if m.formFocusIndex == 5 { // Save
			return m.saveClusterForm()
		}
		if m.formFocusIndex == 6 { // Cancel
			m.state = stateMainMenu
			return m, nil
		}
		// Advance to next field on Enter inside text field
		m.formFocusIndex = (m.formFocusIndex + 1) % totalFormElements
		m.updateFormFocus()
		return m, nil
	case " ":
		if m.formFocusIndex == 4 {
			m.formSSLVerify = !m.formSSLVerify
			return m, nil
		}
	case "esc":
		m.state = stateMainMenu
		return m, nil
	}

	// Update text inputs
	if m.formFocusIndex >= 0 && m.formFocusIndex <= 3 {
		var cmd tea.Cmd
		m.formInputs[m.formFocusIndex], cmd = m.formInputs[m.formFocusIndex].Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *menuModel) updateFormFocus() {
	for i := range m.formInputs {
		if i == m.formFocusIndex {
			m.formInputs[i].Focus()
		} else {
			m.formInputs[i].Blur()
		}
	}
}

func (m *menuModel) saveClusterForm() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.formInputs[0].Value())
	endpoint := strings.TrimSpace(m.formInputs[1].Value())

	if name == "" {
		m.notice = "Cluster name cannot be empty"
		m.noticeType = "error"
		return m, nil
	}
	if endpoint == "" {
		endpoint = "https://localhost.direct"
	}

	cl := ClusterConfig{
		Endpoint:     endpoint,
		AuthUser:     strings.TrimSpace(m.formInputs[2].Value()),
		AuthPassword: strings.TrimSpace(m.formInputs[3].Value()),
		SSLVerify:    m.formSSLVerify,
	}

	// If renamed, remove old
	if m.targetClusterName != "" && m.targetClusterName != name {
		delete(m.cfg.Clusters, m.targetClusterName)
		if m.cfg.CurrentCluster == m.targetClusterName {
			m.cfg.CurrentCluster = name
		}
	}

	m.cfg.Clusters[name] = cl
	if m.cfg.CurrentCluster == "" {
		m.cfg.CurrentCluster = name
	}

	m.dirty = true
	m.notice = fmt.Sprintf("Cluster '%s' saved successfully", name)
	m.noticeType = "success"
	m.state = stateMainMenu
	return m, nil
}

func (m *menuModel) updateDeleteClusterList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	total := len(m.clusterList) + 1
	switch msg.String() {
	case "up", "k":
		if m.clusterListCursor > 0 {
			m.clusterListCursor--
		} else {
			m.clusterListCursor = total - 1
		}
	case "down", "j":
		if m.clusterListCursor < total-1 {
			m.clusterListCursor++
		} else {
			m.clusterListCursor = 0
		}
	case "enter":
		if m.clusterListCursor == len(m.clusterList) {
			m.state = stateMainMenu
			return m, nil
		}
		m.targetClusterName = m.clusterList[m.clusterListCursor]
		m.state = stateConfirmDeleteCluster
	case "esc", "q":
		m.state = stateMainMenu
	}
	return m, nil
}

func (m *menuModel) updateConfirmDeleteCluster(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch strings.ToLower(msg.String()) {
	case "y", "enter":
		delete(m.cfg.Clusters, m.targetClusterName)
		if m.cfg.CurrentCluster == m.targetClusterName {
			m.cfg.CurrentCluster = ""
			for k := range m.cfg.Clusters {
				m.cfg.CurrentCluster = k
				break
			}
		}
		m.dirty = true
		m.notice = fmt.Sprintf("Cluster '%s' deleted", m.targetClusterName)
		m.noticeType = "warn"
		m.state = stateMainMenu
	case "n", "esc", "q":
		m.state = stateMainMenu
	}
	return m, nil
}

func (m *menuModel) refreshDefaultsItems() {
	m.defaultsItems = []string{
		fmt.Sprintf("Model Name:            %s", m.cfg.Defaults.Model),
		fmt.Sprintf("Kubernetes Namespace:  %s", m.cfg.Defaults.Namespace),
		fmt.Sprintf("HTTP Verification Port: %d", m.cfg.Defaults.HTTPPort),
		fmt.Sprintf("gRPC ext_proc Port:    %d", m.cfg.Defaults.GRPCPort),
		fmt.Sprintf("Docker Mode:           %t", m.cfg.Defaults.Docker),
		"< Back to Main Menu",
	}
}

func (m *menuModel) updateEditDefaults(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	total := len(m.defaultsItems)
	switch msg.String() {
	case "up", "k":
		if m.defaultsCursor > 0 {
			m.defaultsCursor--
		} else {
			m.defaultsCursor = total - 1
		}
	case "down", "j":
		if m.defaultsCursor < total-1 {
			m.defaultsCursor++
		} else {
			m.defaultsCursor = 0
		}
	case "enter":
		switch m.defaultsCursor {
		case 0:
			m.activeField = fieldModel
			m.fieldInput = textinput.New()
			m.fieldInput.Placeholder = "qwen2-5-05b-instruct"
			m.fieldInput.SetValue(m.cfg.Defaults.Model)
			m.fieldInput.Focus()
			m.state = stateEditDefaultField
		case 1:
			m.activeField = fieldNamespace
			m.fieldInput = textinput.New()
			m.fieldInput.Placeholder = "oscar-svc"
			m.fieldInput.SetValue(m.cfg.Defaults.Namespace)
			m.fieldInput.Focus()
			m.state = stateEditDefaultField
		case 2:
			m.activeField = fieldHTTPPort
			m.fieldInput = textinput.New()
			m.fieldInput.Placeholder = "9091"
			m.fieldInput.SetValue(strconv.Itoa(m.cfg.Defaults.HTTPPort))
			m.fieldInput.Focus()
			m.state = stateEditDefaultField
		case 3:
			m.activeField = fieldGRPCPort
			m.fieldInput = textinput.New()
			m.fieldInput.Placeholder = "9002"
			m.fieldInput.SetValue(strconv.Itoa(m.cfg.Defaults.GRPCPort))
			m.fieldInput.Focus()
			m.state = stateEditDefaultField
		case 4:
			m.cfg.Defaults.Docker = !m.cfg.Defaults.Docker
			m.dirty = true
			m.refreshDefaultsItems()
		case 5:
			m.state = stateMainMenu
		}
	case "esc", "q":
		m.state = stateMainMenu
	}
	return m, nil
}

func (m *menuModel) updateEditDefaultField(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		val := strings.TrimSpace(m.fieldInput.Value())
		switch m.activeField {
		case fieldModel:
			if val != "" {
				m.cfg.Defaults.Model = val
				m.dirty = true
			}
		case fieldNamespace:
			if val != "" {
				m.cfg.Defaults.Namespace = val
				m.dirty = true
			}
		case fieldHTTPPort:
			if p, err := strconv.Atoi(val); err == nil && p > 0 {
				m.cfg.Defaults.HTTPPort = p
				m.dirty = true
			}
		case fieldGRPCPort:
			if p, err := strconv.Atoi(val); err == nil && p > 0 {
				m.cfg.Defaults.GRPCPort = p
				m.dirty = true
			}
		}
		m.refreshDefaultsItems()
		m.state = stateEditDefaults
		return m, nil
	case "esc":
		m.state = stateEditDefaults
		return m, nil
	}

	var cmd tea.Cmd
	m.fieldInput, cmd = m.fieldInput.Update(msg)
	return m, cmd
}

func (m *menuModel) updateShowYAML(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "esc", "q":
		m.state = stateMainMenu
	}
	return m, nil
}

func (m *menuModel) updateConfirmExit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	total := len(m.confirmExitList)
	switch msg.String() {
	case "up", "k":
		if m.confirmExitIdx > 0 {
			m.confirmExitIdx--
		} else {
			m.confirmExitIdx = total - 1
		}
	case "down", "j":
		if m.confirmExitIdx < total-1 {
			m.confirmExitIdx++
		} else {
			m.confirmExitIdx = 0
		}
	case "enter":
		switch m.confirmExitIdx {
		case 0: // Save and exit
			_ = SaveFileConfig(m.path, m.cfg)
			return m, tea.Quit
		case 1: // Discard and exit
			return m, tea.Quit
		case 2: // Cancel
			m.state = stateMainMenu
		}
	case "esc":
		m.state = stateMainMenu
	}
	return m, nil
}

func (m *menuModel) getSortedClusterNames() []string {
	var names []string
	for k := range m.cfg.Clusters {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// --- View Rendering ---

func (m *menuModel) View() string {
	var b strings.Builder

	// Top banner
	b.WriteString(headerStyle.Render("OSCAR-IM Configuration Manager"))
	b.WriteString("\n")

	// Context metadata card
	activeName, activeCluster, ok := m.cfg.GetActiveCluster()
	endpoint := "none"
	if ok && activeCluster.Endpoint != "" {
		endpoint = activeCluster.Endpoint
	}

	metaCard := fmt.Sprintf(
		"%s %s\n%s %s (%s)\n%s %s  •  %s %s",
		metaLabelStyle.Render("Config File:   "), metaValueStyle.Render(m.path),
		metaLabelStyle.Render("Active Cluster:"), metaValueStyle.Render(activeName), normalItemStyle.Render(endpoint),
		metaLabelStyle.Render("Target Model:  "), metaValueStyle.Render(m.cfg.Defaults.Model),
		metaLabelStyle.Render("Namespace:     "), metaValueStyle.Render(m.cfg.Defaults.Namespace),
	)
	b.WriteString(borderBoxStyle.Render(metaCard))
	b.WriteString("\n")

	// Notice line
	if m.notice != "" {
		switch m.noticeType {
		case "success":
			b.WriteString(successNoticeStyle.Render("[✓] " + m.notice))
		case "error":
			b.WriteString(errorNoticeStyle.Render("[✗] " + m.notice))
		default:
			b.WriteString(warnNoticeStyle.Render("[!] " + m.notice))
		}
		b.WriteString("\n\n")
	} else if m.dirty {
		b.WriteString(warnNoticeStyle.Render("[!] Unsaved changes pending (press 's' or use menu to save)"))
		b.WriteString("\n\n")
	}

	// Body screen
	switch m.state {
	case stateMainMenu:
		b.WriteString(m.renderList(m.mainMenuItems, m.mainCursor))
		b.WriteString(helpStyle.Render("↑/k: up • ↓/j: down • enter: select • s: save • q: exit"))

	case stateSwitchCluster:
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#818CF8")).Render("Select an OSCAR cluster to activate:\n\n"))
		var items []string
		for _, name := range m.clusterList {
			prefix := "[ ] "
			if name == m.cfg.CurrentCluster {
				prefix = "[✓] "
			}
			cl := m.cfg.Clusters[name]
			items = append(items, fmt.Sprintf("%s%-14s -> %s", prefix, name, cl.Endpoint))
		}
		items = append(items, "< Back to Main Menu")
		b.WriteString(m.renderList(items, m.clusterListCursor))
		b.WriteString(helpStyle.Render("↑/k: up • ↓/j: down • enter: activate • esc: back"))

	case stateClusterListForEdit:
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#818CF8")).Render("Select a cluster to edit or add a new one:\n\n"))
		var items []string
		for _, name := range m.clusterList {
			cl := m.cfg.Clusters[name]
			items = append(items, fmt.Sprintf("%-16s (%s)", name, cl.Endpoint))
		}
		items = append(items, "[+ Add New Cluster]")
		items = append(items, "< Back to Main Menu")
		b.WriteString(m.renderList(items, m.clusterListCursor))
		b.WriteString(helpStyle.Render("↑/k: up • ↓/j: down • enter: choose • esc: back"))

	case stateClusterForm:
		title := "Add New Cluster"
		if m.targetClusterName != "" {
			title = fmt.Sprintf("Edit Cluster '%s'", m.targetClusterName)
		}
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#818CF8")).Render(title + "\n\n"))

		labels := []string{
			"Cluster Name / ID:   ",
			"Endpoint URL:        ",
			"Auth Username:       ",
			"Auth Password/Token: ",
		}
		for i := 0; i < 4; i++ {
			cursor := "  "
			if m.formFocusIndex == i {
				cursor = activeCursorStyle.Render("▸ ")
			}
			b.WriteString(fmt.Sprintf("%s%s%s\n", cursor, metaLabelStyle.Render(labels[i]), m.formInputs[i].View()))
		}

		// SSL Verify toggle
		sslCursor := "  "
		if m.formFocusIndex == 4 {
			sslCursor = activeCursorStyle.Render("▸ ")
		}
		sslStatus := normalItemStyle.Render("[ ] disabled")
		if m.formSSLVerify {
			sslStatus = successNoticeStyle.Render("[✓] enabled")
		}
		b.WriteString(fmt.Sprintf("%s%s%s\n\n", sslCursor, metaLabelStyle.Render("SSL Verification:    "), sslStatus))

		// Buttons
		saveCursor := "  "
		if m.formFocusIndex == 5 {
			saveCursor = activeCursorStyle.Render("▸ ")
		}
		cancelCursor := "  "
		if m.formFocusIndex == 6 {
			cancelCursor = activeCursorStyle.Render("▸ ")
		}
		b.WriteString(fmt.Sprintf("%s%s\n", saveCursor, selectedItemStyle.Render("[ Save Cluster ]")))
		b.WriteString(fmt.Sprintf("%s%s\n", cancelCursor, normalItemStyle.Render("[ Cancel ]")))

		b.WriteString(helpStyle.Render("tab/↓: next field • shift-tab/↑: prev field • space: toggle SSL • ctrl+s: save • esc: cancel"))

	case stateDeleteClusterList:
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F87171")).Render("Select a cluster to DELETE:\n\n"))
		var items []string
		for _, name := range m.clusterList {
			items = append(items, fmt.Sprintf("%-16s (%s)", name, m.cfg.Clusters[name].Endpoint))
		}
		items = append(items, "< Back to Main Menu")
		b.WriteString(m.renderList(items, m.clusterListCursor))
		b.WriteString(helpStyle.Render("↑/k: up • ↓/j: down • enter: delete • esc: cancel"))

	case stateConfirmDeleteCluster:
		b.WriteString(errorNoticeStyle.Render(fmt.Sprintf("Are you sure you want to permanently delete cluster '%s'?", m.targetClusterName)))
		b.WriteString("\n\n")
		b.WriteString(metaValueStyle.Render("Press [y / enter] to confirm, or [n / esc] to cancel.\n"))

	case stateEditDefaults:
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#818CF8")).Render("Edit runtime defaults:\n\n"))
		b.WriteString(m.renderList(m.defaultsItems, m.defaultsCursor))
		b.WriteString(helpStyle.Render("↑/k: up • ↓/j: down • enter: edit/toggle • esc: back"))

	case stateEditDefaultField:
		title := "Editing runtime default"
		switch m.activeField {
		case fieldModel:
			title = "Enter Target LLM Model Name:"
		case fieldNamespace:
			title = "Enter Target Kubernetes Namespace:"
		case fieldHTTPPort:
			title = "Enter EPP HTTP Verification Port:"
		case fieldGRPCPort:
			title = "Enter EPP gRPC ext_proc Port:"
		}
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#818CF8")).Render(title + "\n\n"))
		b.WriteString(activeCursorStyle.Render("▸ ") + m.fieldInput.View() + "\n\n")
		b.WriteString(helpStyle.Render("enter: apply • esc: cancel"))

	case stateShowYAML:
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#818CF8")).Render("Configuration YAML Preview:\n\n"))
		encoded, _ := yaml.Marshal(m.cfg)
		b.WriteString(normalItemStyle.Render(string(encoded)))
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("enter / esc / q: return to main menu"))

	case stateConfirmExit:
		b.WriteString(warnNoticeStyle.Render("You have unsaved changes. What would you like to do?\n\n"))
		b.WriteString(m.renderList(m.confirmExitList, m.confirmExitIdx))
		b.WriteString(helpStyle.Render("↑/k: up • ↓/j: down • enter: choose • esc: return"))
	}

	return b.String()
}

func (m *menuModel) renderList(items []string, cursor int) string {
	var b strings.Builder
	for i, item := range items {
		if i == cursor {
			b.WriteString(activeCursorStyle.Render("▸ ") + selectedItemStyle.Render(item) + "\n")
		} else {
			b.WriteString("  " + normalItemStyle.Render(item) + "\n")
		}
	}
	b.WriteString("\n")
	return b.String()
}
