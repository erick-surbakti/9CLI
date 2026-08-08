package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorBG       = lipgloss.Color("235")
	colorBorder   = lipgloss.Color("238")
	colorBorderHi = lipgloss.Color("99")
	colorMuted    = lipgloss.Color("245")
	colorDim      = lipgloss.Color("241")
	colorUser     = lipgloss.Color("117")
	colorUserBG   = lipgloss.Color("237")
	colorAI       = lipgloss.Color("159")
	colorAIBG     = lipgloss.Color("236")
	colorError    = lipgloss.Color("203")
	colorErrorBG  = lipgloss.Color("52")
	colorInfo     = lipgloss.Color("117")
	colorInfoBG   = lipgloss.Color("237")
	colorTitle    = lipgloss.Color("255")
	colorAccent   = lipgloss.Color("99")
	colorPrompt   = lipgloss.Color("141")
	colorSuccess  = lipgloss.Color("114")
	colorPicker   = lipgloss.Color("141")
	colorPickerBG = lipgloss.Color("237")

	appStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorderHi).
			Padding(0, 1)

	headerStyle = lipgloss.NewStyle().
			BorderBottom(true).
			BorderForeground(colorBorder).
			Padding(0, 1).
			MarginBottom(0)

	headerTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorTitle)

	headerBadgeStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Background(colorBG).
				Padding(0, 1).
				Bold(true)

	headerModelStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	userBubbleStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(colorUser).
			Padding(0, 1).
			MarginBottom(1)

	assistantBubbleStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder(), false, false, false, true).
				BorderForeground(colorAI).
				Padding(0, 1).
				MarginBottom(1)

	errorBubbleStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder(), false, false, false, true).
				BorderForeground(colorError).
				Background(colorErrorBG).
				Padding(0, 1).
				MarginBottom(1)

	infoBubbleStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(colorInfo).
			Padding(0, 1).
			MarginBottom(1)

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
			Padding(0, 1).
			MarginTop(0)

	promptStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrompt)

	footerStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	statusStyle = lipgloss.NewStyle().
			Foreground(colorError)

	statusOKStyle = lipgloss.NewStyle().
			Foreground(colorSuccess)

	pickerBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPicker).
			Padding(1, 2)

	pickerTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorPicker)

	pickerItemStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(0, 1)

	pickerActiveStyle = lipgloss.NewStyle().
				Foreground(colorTitle).
				Background(colorPickerBG).
				Bold(true).
				Padding(0, 1)

	pickerProviderStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	pickerHintStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	splashTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorTitle)

	splashSubtitleStyle = lipgloss.NewStyle().
				Foreground(colorMuted)

	splashSpinnerStyle = lipgloss.NewStyle().
				Foreground(colorAccent)

	typingStyle = lipgloss.NewStyle().
			Foreground(colorAI)

	suggestBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorderHi).
			Padding(0, 1).
			MarginTop(1)

	suggestHintStyle = lipgloss.NewStyle().
				Foreground(colorDim)
)
