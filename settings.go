package visualtelemetry

import (
	"regexp"
	"strings"

	"github.com/pacenote-sim/plugin"
)

// What the operator fills in: what the chart is called, whose it is, and the
// colour it is drawn in — the header of every chart is the team's — and how
// long the laps behind the charts are kept.
const (
	SettingTitle    = "title"
	SettingTeam     = "team"
	SettingAccent   = "accent"
	SettingKeepDays = "keep_days"
)

// Defaults, repeated here for a host that sends nothing.
const (
	DefaultTitle    = "TELEMETRY ANALYSIS"
	DefaultAccent   = "#FFFFFF"
	DefaultKeepDays = 90
)

// hexColour is a colour as CSS spells it, six digits.
var hexColour = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// Settings is what the panel renders.
func Settings() []plugin.Setting {
	return []plugin.Setting{
		{
			Name:        SettingTitle,
			Label:       "Chart title",
			Help:        "The words across the top of every chart.",
			Kind:        plugin.KindText,
			Default:     DefaultTitle,
			Placeholder: DefaultTitle,
		},
		{
			Name:        SettingTeam,
			Label:       "Team name",
			Help:        "Shown under the title. Empty shows nothing.",
			Kind:        plugin.KindText,
			Placeholder: "Iberian GT",
		},
		{
			Name:        SettingAccent,
			Label:       "Team colour",
			Help:        "The colour the first lap on a chart is drawn in, as #RRGGBB. White, like the panel, unless the team has one. The others are green, red and blue.",
			Kind:        plugin.KindText,
			Default:     DefaultAccent,
			Placeholder: DefaultAccent,
		},
		{
			Name:        SettingKeepDays,
			Label:       "Keep laps for",
			Help:        "Days. A lap's trace is a few tens of kilobytes; a season is about 180 days. Zero keeps them for ever.",
			Kind:        plugin.KindNumber,
			Default:     "90",
			Placeholder: "90",
		},
	}
}

// config is the settings as this plugin uses them.
type config struct {
	title    string
	team     string
	accent   string
	keepDays int
}

// configOf reads a call's settings, with the defaults where the host sent
// nothing and where a colour is not one.
func configOf(values plugin.Values) config {
	c := config{
		title:    strings.TrimSpace(values.String(SettingTitle)),
		team:     strings.TrimSpace(values.String(SettingTeam)),
		accent:   strings.ToUpper(strings.TrimSpace(values.String(SettingAccent))),
		keepDays: DefaultKeepDays,
	}
	if c.title == "" {
		c.title = DefaultTitle
	}
	if !hexColour.MatchString(c.accent) {
		c.accent = DefaultAccent
	}
	if days, ok := values.Int(SettingKeepDays); ok {
		c.keepDays = days
	}
	return c
}
