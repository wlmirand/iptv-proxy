/*
 * Iptv-Proxy is a project to proxyfie an m3u file and to proxyfie an Xtream iptv service (client API).
 * Copyright (C) 2020  Pierre-Emmanuel Jacquier
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <https://www.gnu.org/licenses/>.
 */

package server

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/pierre-emmanuelJ/iptv-proxy/pkg/config"
	"github.com/pierre-emmanuelJ/iptv-proxy/pkg/filter"
)

// Summary describes what the proxy runs with, for its log at startup. It
// names no password and no address with credentials: an input is its host
// only, a user is its name.
func (c *Config) Summary() string {
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }

	line("iptv-proxy %s", c.Version)
	line("")
	line("Hostname:       %s", c.HostConfig.Hostname)
	line("Listening on:   %s", c.listenAddress())
	line("Addresses as:   %s", c.proxyBaseURL())
	if c.UserAgent != "" {
		line("User-Agent:     %s", c.UserAgent)
	} else {
		line("User-Agent:     the player's own")
	}
	line("Playlist cache: %d hour(s)", c.M3UCacheExpiration)
	if c.NoStreamSharing {
		line("Live streams:   one provider connection per player")
	} else {
		line("Live streams:   one provider connection shared between players")
	}
	if c.StatusPassword != "" {
		line("Status page:    /status")
	}
	if c.ProxyLogos {
		line("Logos:          through the proxy")
	}
	if text := patternsText(c.Filter); text != "" {
		line("Filters:        %s", text)
	}

	line("")
	line("Users:")
	if c.XtreamPassthrough {
		line("  - each player's own provider account")
	}
	for _, u := range c.users {
		details := []string{}
		if u.max > 0 {
			details = append(details, fmt.Sprintf("%d streams at once", u.max))
		}
		for _, configured := range c.Users {
			if configured.Name == u.name.String() {
				if text := patternsText(configured.Filter); text != "" {
					details = append(details, "filters: "+text)
				}
			}
		}
		line("  - %s%s", u.name, parenthesised(details))
	}

	line("")
	line("Inputs:")
	c.summarizeInputs(line)

	line("")
	line("Output:")
	if c.Output == nil {
		line("  Formats:            everything the inputs allow")
	} else if len(c.Output.Formats) == 0 {
		line("  Formats:            none")
	} else {
		line("  Formats:            %s", strings.Join(c.Output.Formats, ", "))
	}
	if len(c.sources) > 1 {
		if c.NoCombined {
			line("  Combined catalogue: no")
		} else {
			line("  Combined catalogue: yes")
			dedupe := "none"
			if c.Dedupe != config.DedupeNone {
				dedupe = config.DedupeGuideID
			}
			line("  Dedupe:             %s", dedupe)
			if c.hasPrefix() {
				line("  Category separator: %q", c.Output.Separator())
			}
		}
	}
	for _, v := range c.views {
		line("  View:               /%s", strings.Trim(v.CustomEndpoint, "/"))
	}
	if c.HDHomeRunPort != 0 {
		line("  HDHomeRun:          port %d, %s", c.HDHomeRunPort, tunerText(c.HDHomeRunTuners, "every input"))
	}
	for _, t := range c.tuners {
		what := "every input"
		if t.view && len(t.sources) == 1 {
			what = t.sources[0].name
		}
		line("  HDHomeRun:          port %d, %s", t.HDHomeRunPort, tunerText(t.HDHomeRunTuners, what))
	}
	return b.String()
}

func (c *Config) summarizeInputs(line func(string, ...any)) {
	merged := len(c.sources) > 1
	switch {
	case len(c.sources) > 0:
		for _, src := range c.sources {
			details := []string{}
			if merged {
				details = append(details, fmt.Sprintf("ids from %d", src.base))
			}
			if src.prefix != "" {
				details = append(details, fmt.Sprintf("category prefix %q", src.prefix))
			}
			if src.individual {
				details = append(details, "also at /"+src.path)
			}
			if src.max > 0 {
				details = append(details, fmt.Sprintf("%d streams at once", src.max))
			}
			kind, host := "xtream", hostOf(src.account.BaseURL)
			if src.m3u != nil {
				kind, host = "m3u", hostOf(src.m3u.address)
			}
			line("  - %s · %s · %s%s", src.name, kind, host, parenthesised(details))
		}
	case c.XtreamPassthrough:
		line("  - xtream passthrough · %s", hostOf(c.XtreamBaseURL))
	default:
		if c.XtreamBaseURL != "" {
			name := c.XtreamName
			if name == "" {
				name = "xtream"
			}
			line("  - %s · xtream · %s", name, hostOf(c.XtreamBaseURL))
		}
		if c.servesM3U() {
			line("  - m3u · %s", hostOf(c.RemoteURL.String()))
		}
	}
}

func (c *Config) hasPrefix() bool {
	for _, src := range c.sources {
		if src.prefix != "" {
			return true
		}
	}
	return false
}

// hostOf is the scheme and host of an address, or "file" for a local file:
// what follows may hold credentials.
func hostOf(address string) string {
	u, err := url.Parse(address)
	if err != nil || u.Host == "" {
		if strings.HasPrefix(address, "http://") || strings.HasPrefix(address, "https://") {
			return "(invalid address)"
		}
		return "local file"
	}
	return u.Scheme + "://" + u.Host
}

func tunerText(count int, what string) string {
	if count > 0 {
		return fmt.Sprintf("%s, %d tuners", what, count)
	}
	return what
}

func parenthesised(details []string) string {
	if len(details) == 0 {
		return ""
	}
	return " (" + strings.Join(details, ", ") + ")"
}

func patternsText(p filter.Patterns) string {
	var parts []string
	for _, f := range []struct{ name, value string }{
		{"group", p.Group}, {"channel", p.Channel}, {"group-exclude", p.GroupExclude}, {"channel-exclude", p.ChannelExclude},
	} {
		if f.value != "" {
			parts = append(parts, fmt.Sprintf("%s %q", f.name, f.value))
		}
	}
	return strings.Join(parts, ", ")
}
