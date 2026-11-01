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
	"strings"
	"testing"

	"github.com/pierre-emmanuelJ/iptv-proxy/pkg/config"
	"github.com/pierre-emmanuelJ/iptv-proxy/pkg/filter"
)

func TestSummary(t *testing.T) {
	p := newProvider(t)
	conf := &config.ProxyConfig{
		HostConfig: &config.HostConfiguration{Hostname: "tv.example", Port: 8080}, AdvertisedPort: 443, HTTPS: true,
		XtreamBaseURL: p.URL, XtreamUser: xUser, XtreamPassword: xPass, XtreamName: "Brazil",
		XtreamCategoryPrefix: "BR", XtreamPath: "brazil", XtreamIndividual: true,
		User: user, Password: pass, M3UFileName: "iptv.m3u", M3UCacheExpiration: 1, UserAgent: "VLC/3.0.20 LibVLC/3.0.20",
		Users: []config.User{
			{Name: "ana", Password: "ana-secret-pw", MaxConnections: 2, Filter: filter.Patterns{Group: "^BR "}},
			{Name: "bob", Password: "bob-secret-pw"},
		},
		Sources: []config.Source{{
			Name: "Portugal", Type: config.SourceM3U, CategoryPrefix: "PT",
			URL: p.URL + "/list.m3u?token=url-secret-token",
		}},
		Output: &config.Output{Formats: []string{config.OutputM3U, config.OutputXMLTV}, CategorySeparator: " - "},
		Dedupe: config.DedupeNone,
		Tuners: []config.Tuner{{Port: 5004, Count: 2}, {Port: 5005, Source: "Portugal"}},
	}
	c, err := NewServer(conf)
	if err != nil {
		t.Fatal(err)
	}
	summary := c.Summary()

	for _, want := range []string{
		"Hostname:       tv.example",
		"Addresses as:   https://tv.example:443",
		"User-Agent:     VLC/3.0.20 LibVLC/3.0.20",
		"- ana (2 streams at once, filters: group \"^BR \")",
		"- bob\n",
		"- Brazil · xtream · " + hostOf(p.URL),
		"- Portugal · m3u · " + hostOf(p.URL),
		`category prefix "PT"`,
		"also at /brazil",
		"Formats:            m3u, xmltv",
		"Dedupe:             none",
		`Category separator: " - "`,
		"View:               /brazil",
		"HDHomeRun:          port 5004, every input, 2 tuners",
		"HDHomeRun:          port 5005, Portugal",
	} {
		if !strings.Contains(summary, want) {
			t.Errorf("the summary lacks %q:\n%s", want, summary)
		}
	}

	// no secret of any kind
	for _, secret := range []string{xUser, xPass, pass, "ana-secret-pw", "bob-secret-pw", "url-secret-token", "/list.m3u"} {
		if strings.Contains(summary, secret) {
			t.Errorf("the summary gives away %q:\n%s", secret, summary)
		}
	}
}

func TestSummaryOfASingleXtreamAccount(t *testing.T) {
	c, err := NewServer(&config.ProxyConfig{
		HostConfig: &config.HostConfiguration{Hostname: "tv.example", Port: 8080}, AdvertisedPort: 8080,
		XtreamBaseURL: "http://provider.example:8080", XtreamUser: xUser, XtreamPassword: xPass,
		User: user, Password: pass, M3UFileName: "iptv.m3u", M3UCacheExpiration: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	summary := c.Summary()
	for _, want := range []string{"Inputs:\n  - xtream · xtream · http://provider.example:8080", "- me\n", "User-Agent:     the player's own", "Formats:            everything the inputs allow"} {
		if !strings.Contains(summary, want) {
			t.Errorf("the summary lacks %q:\n%s", want, summary)
		}
	}
	if strings.Contains(summary, "Dedupe") {
		t.Errorf("a single account has no dedupe:\n%s", summary)
	}
}
