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
	"net/http"
	"testing"

	"github.com/pierre-emmanuelJ/iptv-proxy/pkg/config"
)

// codeOf asks for a path and returns the HTTP status.
func codeOf(t *testing.T, base, path string) int {
	t.Helper()
	resp, _ := get(t, base+path)
	return resp.StatusCode
}

func TestOutputFormats(t *testing.T) {
	const (
		api      = "/player_api.php?" + creds
		playlist = "/iptv.m3u?" + creds
		getphp   = "/get.php?" + creds + "&type=m3u_plus&output=ts"
		guide    = "/xmltv.php?" + creds
		stream   = "/live/me/secret/1.ts"
	)
	all := []string{config.OutputXtream, config.OutputM3U, config.OutputXMLTV}

	for name, c := range map[string]struct {
		formats []string
		served  []string
		absent  []string
	}{
		"no output section: everything": {
			formats: nil,
			served:  []string{api, playlist, getphp, guide, stream},
		},
		"all the formats": {
			formats: all,
			served:  []string{api, playlist, getphp, guide, stream},
		},
		"xtream alone": {
			formats: []string{config.OutputXtream},
			served:  []string{api, getphp, stream},
			absent:  []string{playlist, guide},
		},
		"m3u alone: the playlist and the streams it names": {
			formats: []string{config.OutputM3U},
			served:  []string{playlist, stream},
			absent:  []string{api, getphp, guide},
		},
		"xmltv alone": {
			formats: []string{config.OutputXMLTV},
			served:  []string{guide},
			absent:  []string{api, getphp, playlist, stream},
		},
		"nothing": {
			formats: []string{},
			absent:  []string{api, getphp, playlist, guide, stream},
		},
	} {
		p := newProvider(t)
		base := proxy(t, p, func(conf *config.ProxyConfig) {
			conf.Users = []config.User{{Name: "me", Password: "secret"}}
			if c.formats != nil {
				conf.Output = &config.Output{Formats: c.formats}
			}
		})
		for _, path := range c.served {
			if got := codeOf(t, base, path); got == http.StatusNotFound {
				t.Errorf("%s: %s is not served", name, path)
			}
		}
		for _, path := range c.absent {
			if got := codeOf(t, base, path); got != http.StatusNotFound {
				t.Errorf("%s: %s is served (status %d)", name, path, got)
			}
		}
	}
}

func TestOutputXtreamNeedsAnXtreamInput(t *testing.T) {
	conf := &config.ProxyConfig{
		HostConfig:         &config.HostConfiguration{Hostname: "proxy.example", Port: 8080},
		User:               user,
		Password:           pass,
		M3UFileName:        "iptv.m3u",
		M3UCacheExpiration: 1,
		Output:             &config.Output{Formats: []string{config.OutputXtream}},
	}
	if _, err := NewServer(conf); err == nil {
		t.Error("an xtream output with no Xtream input is accepted")
	}
}
