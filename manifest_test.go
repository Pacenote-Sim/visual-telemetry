package visualtelemetry_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pacenote-sim/plugin"
)

func TestTheHostCanLoadThisManifest(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	m, err := plugin.LoadManifest(".")
	r.NoError(err)
	r.NoError(m.Validate())
	r.Equal("visual-telemetry", m.Name)
	r.Equal("visual-telemetry", m.Binary)
	r.Equal(plugin.InterfaceVersion, m.InterfaceVersion)

	r.Empty(m.Capabilities.Events, "it asks for no events: the client posts")
	r.Empty(m.Capabilities.Requests)
	r.False(m.Capabilities.Network, "it draws; it calls nothing")
	r.Empty(m.Capabilities.Calls)
	r.True(m.Capabilities.Database)
	r.True(m.Capabilities.ReadsDriverData, "the legend names the driver")

	for path, want := range map[string]plugin.Access{
		"/":                   plugin.AccessAdmin,
		"/me":                 plugin.AccessDriver,
		"/style.css":          plugin.AccessPublic,
		"/laps":               plugin.AccessDriver,
		"/charts":             plugin.AccessCustom,
		"/charts/s/7.svg":     plugin.AccessCustom,
		"/charts/compare.png": plugin.AccessCustom,
	} {
		got, ok := m.Capabilities.HTTP.For(path)
		r.Truef(ok, "%s is served and not declared", path)
		r.Equalf(want, got, "%s is declared for the wrong caller", path)
	}
	// The one route the plugin decides for itself says why, in words.
	for _, route := range m.Capabilities.HTTP.Routes {
		if route.Path == "/charts" {
			r.Contains(route.Reason, "administrator")
		}
	}
}
