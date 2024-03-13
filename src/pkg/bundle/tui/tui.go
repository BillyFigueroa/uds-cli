package tui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/defenseunicorns/uds-cli/src/pkg/utils"
	"github.com/defenseunicorns/zarf/src/config"
	"github.com/defenseunicorns/zarf/src/pkg/k8s"
	"github.com/defenseunicorns/zarf/src/pkg/message"
	"github.com/defenseunicorns/zarf/src/types"
	"github.com/fatih/color"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/printer"
)

// todo: watch naming collisions, spinners also has a TickMsg
type tickMsg time.Time
type operation string

// todo: make dynamic?
const WIDTH = 50

const (
	DeployOp operation = "deploy"
)

var (
	Program *tea.Program
)

// private interface to decouple tui pkg from bundle pkg
type bndlClientShim interface {
	Deploy() error
	ClearPaths()
}

type Model struct {
	bndlClient            bndlClientShim
	bundleYAML            string
	completeChan          chan int
	fatalChan             chan error
	progressBars          []progress.Model
	pkgNames              []string
	pkgIdx                int
	totalComponentsPerPkg []int
	totalPkgs             int
	spinners              []spinner.Model
	confirmed             bool
	complete              []bool
	done                  bool
	resetProgress         []bool
}

func InitModel(client bndlClientShim, bundleYAML string) Model {
	var confirmed bool
	if config.CommonOptions.Confirm {
		confirmed = true
	}

	return Model{
		bndlClient:   client,
		completeChan: make(chan int),
		fatalChan:    make(chan error),
		confirmed:    confirmed,
		bundleYAML:   bundleYAML,
	}
}

func (m Model) Init() tea.Cmd {
	cmd := tea.Println(fmt.Sprintf("%s", m.preDeployView()))
	return tea.Sequence(cmd, func() tea.Msg {
		return DeployOp
	})
}

// todo: I think Zarf has this...
func GetDeployedPackage(packageName string) (deployedPackage *types.DeployedPackage) {
	// Get the secret that describes the deployed package
	k8sClient, _ := k8s.New(message.Debugf, k8s.Labels{config.ZarfManagedByLabel: "zarf"})
	secret, err := k8sClient.GetSecret("zarf", config.ZarfPackagePrefix+packageName)
	if err != nil {
		return deployedPackage
	}

	err = json.Unmarshal(secret.Data["data"], &deployedPackage)
	if err != nil {
		panic(0)
	}
	return deployedPackage
}

func pause() tea.Cmd {
	return tea.Tick(time.Millisecond*500, func(_ time.Time) tea.Msg {
		return nil
	})
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmds []tea.Cmd
	)
	select {
	case <-m.fatalChan:
		m.spinners[m.pkgIdx].Spinner.Frames = []string{""}
		m.spinners[m.pkgIdx].Style = lipgloss.NewStyle().SetString("❌")
		s, spinnerCmd := m.spinners[m.pkgIdx].Update(spinner.TickMsg{})
		m.spinners[m.pkgIdx] = s
		return m, tea.Sequence(spinnerCmd, pause(), tea.Quit)
	default:
		switch msg := msg.(type) {
		case progress.FrameMsg:
			progressModel, cmd := m.progressBars[m.pkgIdx].Update(msg)
			m.progressBars[m.pkgIdx] = progressModel.(progress.Model)
			return m, cmd
		case tickMsg:

			// on a successful bundle deployment, print success message and quit
			if len(m.complete) == m.totalPkgs {
				// check if last pkg is complete; if so, print success messages and quit
				m.done = true // remove the current view
				line := strings.Repeat("─", WIDTH) + "\n"
				successCmds := []tea.Cmd{tea.Println(line)}
				for i := 0; i < m.totalPkgs; i++ {
					successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#32A852"))
					successMsg := fmt.Sprintf("✅ Package %s deployed\n", m.pkgNames[i])
					successCmds = append(successCmds, tea.Println(successStyle.Render(successMsg)))
				}

				successCmds = append(successCmds, tea.Quit)
				return m, tea.Sequence(successCmds...)
			}

			// update progress bars and spinners for all pkgs
			var spinnerCmds []tea.Cmd
			var progressCmds []tea.Cmd
			var updateCmds []tea.Cmd
			for i, name := range m.pkgNames {
				var progressCmd tea.Cmd
				if !(len(m.complete) > i && m.complete[i]) {
					deployedPkg := GetDeployedPackage(name)

					// handle upgrade scenario by resetting the progress bar, otherwise increment it
					if m.resetProgress[i] {
						progressCmds = append(progressCmds, m.progressBars[i].SetPercent(0))
						// if upgraded len(deployedPkg.DeployedComponents) will be equal to the number of components in the package
						if deployedPkg != nil && len(deployedPkg.DeployedComponents) == 1 {
							m.resetProgress[i] = false
						}
					} else if deployedPkg != nil && len(m.totalComponentsPerPkg) > i {
						// slight delay between rendering the TUI and updating m.totalComponentsPerPkg, be sure to check for it
						progressCmd = m.progressBars[i].SetPercent(
							min(float64(len(deployedPkg.DeployedComponents))/float64(m.totalComponentsPerPkg[i]), 0.99),
						)
					}
				}

				// for all completed pkgs, ensure spinner is ✅ and progress is full
				// todo: sometimes get (error) on spinner at end of deployment
				for j := range m.complete {
					m.spinners[j].Style = lipgloss.NewStyle().SetString("✅")
					m.spinners[j].Spinner.Frames = []string{""}
					progressCmd = m.progressBars[j].SetPercent(1)
				}
				progressCmds = append(progressCmds, progressCmd)

				// update spinner
				s, cmd := m.spinners[i].Update(spinner.TickMsg{})
				m.spinners[i] = s
				spinnerCmds = append(spinnerCmds, cmd)
			}

			// join spinnerCmds and progressCmds
			updateCmds = append(updateCmds, progressCmds...)
			updateCmds = append(updateCmds, spinnerCmds...)
			updateCmds = append(updateCmds, tickCmd())

			return m, tea.Sequence(updateCmds...)

		case tea.KeyMsg:
			switch msg.String() {
			case "y", "Y":
				if !m.confirmed {
					m.confirmed = true
				}
				return m, func() tea.Msg {
					return DeployOp
				}
			case "n", "N":
				if !m.confirmed {
					m.confirmed = false
				}
			case "ctrl+c", "q":
				return m, tea.Quit
			}

		case operation:
			if m.confirmed {
				deployCmd := func() tea.Msg {
					// if something goes wrong in Deploy(), reset the terminal
					defer utils.GracefulPanic()
					// run Deploy concurrently so we can update the TUI while it runs
					if err := m.bndlClient.Deploy(); err != nil {
						m.bndlClient.ClearPaths()
						m.fatalChan <- fmt.Errorf("failed to deploy bundle: %s", err.Error())
					}
					return nil
				}
				// use a ticker to update the TUI during deployment
				return m, tea.Batch(tickCmd(), deployCmd)
			}

		case string:
			if strings.Split(msg, ":")[0] == "package" {
				// grab pkgName
				pkgName := strings.Split(msg, ":")[1]
				m.pkgNames = append(m.pkgNames, pkgName)

				// if pkg is already deployed, set resetProgress to true to clear the progress bar
				if deployedPkg := GetDeployedPackage(pkgName); deployedPkg != nil && len(deployedPkg.DeployedComponents) != 0 {
					m.resetProgress = append(m.resetProgress, true)
				}
				// grab pkg index
				if currentPkgIdx, err := strconv.Atoi(strings.Split(msg, ":")[2]); err == nil {
					m.pkgIdx = currentPkgIdx
					m.progressBars = append(m.progressBars, progress.New(progress.WithDefaultGradient()))
					s := spinner.New()
					s.Spinner = spinner.Dot
					s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
					m.spinners = append(m.spinners, s)
				}

			} else if strings.Split(msg, ":")[0] == "totalComponents" {
				if totalComponents, err := strconv.Atoi(strings.Split(msg, ":")[1]); err == nil {
					m.totalComponentsPerPkg = append(m.totalComponentsPerPkg, totalComponents)
				}
			} else if strings.Split(msg, ":")[0] == "totalPackages" {
				if totalPkgs, err := strconv.Atoi(strings.Split(msg, ":")[1]); err == nil {
					m.totalPkgs = totalPkgs
				}
			} else if strings.Split(msg, ":")[0] == "complete" {
				m.spinners[m.pkgIdx].Spinner.Frames = []string{""}
				m.spinners[m.pkgIdx].Style = lipgloss.NewStyle().SetString("✅")
				cmds = append(cmds, m.progressBars[m.pkgIdx].SetPercent(1))
				m.complete = append(m.complete, true)
			}
		}
	}

	// keep this as Batch for now, spinner breaks if it's a Sequence
	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	if m.done {
		// clear the controlled Program's output
		return ""
	} else if m.confirmed {
		line := strings.Repeat("─", WIDTH)
		return fmt.Sprintf("%s\n%s", line, m.deployView())
	}
	return ""
}

func (m Model) deployView() string {
	view := ""
	for i := range m.progressBars {
		text := lipgloss.NewStyle().
			Width(WIDTH).
			Align(lipgloss.Left).
			Padding(0, 3).
			Render(fmt.Sprintf("%s Package %s deploying ...", m.spinners[i].View(), m.pkgNames[i]))

		// render progress bar until deployment is complete
		bar := lipgloss.NewStyle().
			Width(WIDTH).
			Align(lipgloss.Left).
			Padding(0, 3).
			MarginTop(1).
			Render(m.progressBars[i].View())

		ui := lipgloss.JoinVertical(lipgloss.Center, text, bar)

		if len(m.complete) > i && m.complete[i] {
			text = lipgloss.NewStyle().
				Width(WIDTH).
				Align(lipgloss.Left).
				Padding(0, 3).
				Render(fmt.Sprintf("%s Package %s deployed", m.spinners[i].View(), m.pkgNames[i]))
			ui = lipgloss.JoinVertical(lipgloss.Center, text, bar)
		}

		boxStyle := lipgloss.NewStyle().Width(WIDTH).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#874BFD")).
			BorderTop(true).
			BorderLeft(true).
			BorderRight(true).
			BorderBottom(true)
		subtle := lipgloss.AdaptiveColor{Light: "#D9DCCF", Dark: "#383838"}
		box := lipgloss.Place(0, 6,
			lipgloss.Left, lipgloss.Top,
			boxStyle.Render(ui),
			lipgloss.WithWhitespaceForeground(subtle),
		)

		if len(m.complete) > i && m.complete[i] {
			boxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#874BFD")).
				BorderTop(true).
				BorderLeft(true).
				BorderRight(true).
				BorderBottom(true)
			box = lipgloss.Place(WIDTH, 6,
				lipgloss.Left, lipgloss.Top,
				boxStyle.Render(ui),
				lipgloss.WithWhitespaceForeground(subtle),
			)
		}
		view = lipgloss.JoinVertical(lipgloss.Center, view, box)
	}

	return view
}

func (m Model) preDeployView() string {
	header := "🎁 BUNDLE DEFINITION"

	prompt := "❓ Deploy this bundle? (y/n)"

	prettyYAML := colorPrintYAML(m.bundleYAML)

	line := strings.Repeat("─", WIDTH)

	// Concatenate header, highlighted YAML, and prompt
	return fmt.Sprintf("\n%s\n\n%s\n\n%s\n\n%s", header, line, prettyYAML, prompt)
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// colorPrintYAML makes a pretty-print YAML string with color
func colorPrintYAML(yaml string) string {
	tokens := lexer.Tokenize(yaml)

	var p printer.Printer
	p.Bool = func() *printer.Property {
		return &printer.Property{
			Prefix: yamlFormat(color.FgHiWhite),
			Suffix: yamlFormat(color.Reset),
		}
	}
	p.Number = func() *printer.Property {
		return &printer.Property{
			Prefix: yamlFormat(color.FgHiWhite),
			Suffix: yamlFormat(color.Reset),
		}
	}
	p.MapKey = func() *printer.Property {
		return &printer.Property{
			Prefix: yamlFormat(color.FgHiCyan),
			Suffix: yamlFormat(color.Reset),
		}
	}
	p.Anchor = func() *printer.Property {
		return &printer.Property{
			Prefix: yamlFormat(color.FgHiYellow),
			Suffix: yamlFormat(color.Reset),
		}
	}
	p.Alias = func() *printer.Property {
		return &printer.Property{
			Prefix: yamlFormat(color.FgHiYellow),
			Suffix: yamlFormat(color.Reset),
		}
	}
	p.String = func() *printer.Property {
		return &printer.Property{
			Prefix: yamlFormat(color.FgHiMagenta),
			Suffix: yamlFormat(color.Reset),
		}
	}

	outputYAML := p.PrintTokens(tokens)

	if config.NoColor {
		// If no color is specified strip any color codes from the output - https://regex101.com/r/YFyIwC/2
		ansiRegex := regexp.MustCompile(`\x1b\[(.*?)m`)
		outputYAML = ansiRegex.ReplaceAllString(outputYAML, "")
	}
	return outputYAML
}

func yamlFormat(attr color.Attribute) string {
	const yamlEscape = "\x1b"
	return fmt.Sprintf("%s[%dm", yamlEscape, attr)
}
