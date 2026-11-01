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
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pierre-emmanuelJ/iptv-proxy/pkg/config"
)

func preloadConfig(t *testing.T, q *provider) *Config {
	t.Helper()
	c, err := NewServer(&config.ProxyConfig{
		HostConfig: &config.HostConfiguration{Hostname: "proxy.example", Port: 8080}, AdvertisedPort: 8080,
		User: user, Password: pass, M3UFileName: "iptv.m3u", M3UCacheExpiration: 1,
		Sources: []config.Source{{Name: "Portugal", Type: config.SourceM3U, URL: q.URL + "/list.m3u"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// The playlists are read as the proxy starts, and the log says so: the
// first client does not wait for them.
func TestPreloadReadsThePlaylists(t *testing.T) {
	logs := captureLogs(t)
	q := newProvider(t)
	c := preloadConfig(t, q)
	c.preload()

	for _, want := range []string{`Reading "Portugal" (m3u, `, `"Portugal" ready: 3 channels, 0 movies, 1 categories (`, "Ready: 1 playlist(s) read in "} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("the log lacks %q:\n%s", want, logs.String())
		}
	}
	if strings.Contains(logs.String(), "list.m3u") {
		t.Errorf("the log names the playlist's path:\n%s", logs.String())
	}

	// a client then finds it read
	srv := httptest.NewServer(c.Handler())
	t.Cleanup(srv.Close)
	if got := len(listOf(t, srv.URL, "get_live_streams", "stream_id")); got != 3 {
		t.Errorf("%d channels", got)
	}
	if n := q.count("/list.m3u"); n != 1 {
		t.Errorf("the playlist was asked %d times, want 1", n)
	}
	if !strings.Contains(logs.String(), "playlist of me built: ") {
		// the playlist of a user is built when asked, and logged then
		get(t, srv.URL+"/iptv.m3u?"+creds)
		if !strings.Contains(logs.String(), "playlist of me built: 3 entries") {
			t.Errorf("the log lacks the playlist built:\n%s", logs.String())
		}
	}
}

// A playlist that cannot be read at the start does not stop the proxy: it is
// read when a client asks.
func TestPreloadWithAFailingPlaylist(t *testing.T) {
	logs := captureLogs(t)
	q := newProvider(t)
	q.down.Store(true)
	c := preloadConfig(t, q)
	c.preload()

	for _, want := range []string{`"Portugal" failed after `, "read again when a client asks", "Ready, with 1 of 1 playlist(s) not read"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("the log lacks %q:\n%s", want, logs.String())
		}
	}

	q.down.Store(false)
	srv := httptest.NewServer(c.Handler())
	t.Cleanup(srv.Close)
	if got := len(listOf(t, srv.URL, "get_live_streams", "stream_id")); got != 3 {
		t.Errorf("after the provider is back: %d channels", got)
	}
}

func TestPreloadWithoutPlaylists(t *testing.T) {
	logs := captureLogs(t)
	c, err := NewServer(&config.ProxyConfig{
		HostConfig: &config.HostConfiguration{Hostname: "proxy.example", Port: 8080}, AdvertisedPort: 8080,
		XtreamBaseURL: "http://provider.example", XtreamUser: xUser, XtreamPassword: xPass,
		User: user, Password: pass, M3UFileName: "iptv.m3u", M3UCacheExpiration: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	c.preload()
	if !strings.Contains(logs.String(), "Ready") || strings.Contains(logs.String(), "Reading") {
		t.Errorf("log:\n%s", logs.String())
	}
}
