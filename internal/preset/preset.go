// Package preset provides the domain-group catalog used by the "block --preset"
// and pomodoro commands: named categories (social, video, news, games) mapped
// to the domains blocked together.
package preset

import (
	"fmt"
	"strings"
)

// Preset is a named group of domains blocked together by category.
type Preset struct {
	Name    string   `json:"name"`
	Label   string   `json:"label"`
	Domains []string `json:"domains"`
}

// builtins is the default catalog. The exact domain list per category is a
// product decision; names are the stable identifiers used by the CLI/IPC.
var builtins = []Preset{
	{
		Name:    "social",
		Label:   "Redes sociais e mensageiros",
		Domains: []string{"twitter.com", "x.com", "facebook.com", "instagram.com", "tiktok.com", "whatsapp.com", "linkedin.com"},
	},
	{
		Name:    "video",
		Label:   "Vídeo e streaming",
		Domains: []string{"youtube.com", "netflix.com", "twitch.tv", "primevideo.com", "disneyplus.com"},
	},
	{
		Name:    "news",
		Label:   "Notícias e fóruns",
		Domains: []string{"reddit.com", "news.ycombinator.com", "cnn.com", "bbc.com", "globo.com"},
	},
	{
		Name:    "games",
		Label:   "Jogos",
		Domains: []string{"steam.com", "epicgames.com", "roblox.com", "xbox.com", "playstation.com"},
	},
}

// List returns a copy of the catalog so callers cannot mutate the builtins.
func List() []Preset {
	out := make([]Preset, len(builtins))
	for i, p := range builtins {
		out[i] = Preset{
			Name:    p.Name,
			Label:   p.Label,
			Domains: append([]string(nil), p.Domains...),
		}
	}
	return out
}

// Resolve returns the preset with the given name (case-insensitive). Unknown
// names return an error listing the available options.
func Resolve(name string) (Preset, error) {
	for _, p := range builtins {
		if strings.EqualFold(p.Name, strings.TrimSpace(name)) {
			return Preset{
				Name:    p.Name,
				Label:   p.Label,
				Domains: append([]string(nil), p.Domains...),
			}, nil
		}
	}
	var names []string
	for _, p := range builtins {
		names = append(names, p.Name)
	}
	return Preset{}, fmt.Errorf("preset desconhecido %q (disponíveis: %s)", name, strings.Join(names, ", "))
}
