package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorBorder  = lipgloss.Color("240")
	colorMuted   = lipgloss.Color("245")
	colorUser    = lipgloss.Color("86")
	colorAI      = lipgloss.Color("159")
	colorError   = lipgloss.Color("203")
	colorInfo    = lipgloss.Color("117")
	colorTitle   = lipgloss.Color("255")
	colorPrompt  = lipgloss.Color("86")

	appStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	headerStyle = lipgloss.NewStyle().
			BorderBottom(true).
			BorderForeground(colorBorder).
			Padding(0, 1)

	headerTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorTitle)

	headerModelStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	userLabelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorUser)

	assistantLabelStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorAI)

	errorLabelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorError)

	infoLabelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorInfo)

	inputBoxStyle = lipgloss.NewStyle().
			BorderTop(true).
			BorderForeground(colorBorder).
			Padding(0, 1)

	promptStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrompt)

	footerStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	statusStyle = lipgloss.NewStyle().
			Foreground(colorError)
)
