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
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/pierre-emmanuelJ/iptv-proxy/pkg/config"
)

// twoWithOneID finds two keys that have the same id.
func twoWithOneID(t *testing.T) (a, b string) {
	t.Helper()
	seen := map[int64]string{}
	for i := range 1_000_000 {
		key := fmt.Sprintf("http://tv.example/channel/%d.ts", i)
		id := hashID(key)
		if other, ok := seen[id]; ok {
			return other, key
		}
		seen[id] = key
	}
	t.Fatal("no two keys share an id")
	return "", ""
}

func TestAssignIDs(t *testing.T) {
	a, b := twoWithOneID(t)
	if a > b {
		a, b = b, a
	}
	own := hashID(a)

	ids := assignIDs([]string{b, "x", a})
	if ids[a] != own {
		t.Errorf("the first address lost its id: %d, want %d", ids[a], own)
	}
	if ids[b] == own || ids[b] == 0 || ids[b] >= sourceIDs {
		t.Errorf("the other address took %d, the id of the first (%d)", ids[b], own)
	}

	// the order of the playlist does not matter
	again := assignIDs([]string{a, "x", b})
	for _, k := range []string{a, b, "x"} {
		if ids[k] != again[k] {
			t.Errorf("%s: %d, then %d", k, ids[k], again[k])
		}
	}

	// an address that collides with nothing keeps its id when others come and go
	more := assignIDs([]string{"x", "y", "z"})
	if more["x"] != ids["x"] || more["x"] != hashID("x") {
		t.Errorf("x moved: %d, %d", ids["x"], more["x"])
	}

	// every address has its own id
	var keys []string
	for i := range 20000 {
		keys = append(keys, "k"+strconv.Itoa(i))
	}
	seen := map[int64]bool{}
	for _, id := range assignIDs(keys) {
		if seen[id] || id < 1 || id >= sourceIDs {
			t.Fatalf("id %d is used twice, or out of range", id)
		}
		seen[id] = true
	}
}

// m3uOnly serves the playlist of q alone, as the only source.
func m3uOnly(t *testing.T, q *provider, mutate func(*config.ProxyConfig)) string {
	t.Helper()
	return proxy(t, q, func(c *config.ProxyConfig) {
		c.XtreamBaseURL, c.XtreamUser, c.XtreamPassword = "", "", ""
		c.Sources = []config.Source{{Name: "portugal", Type: config.SourceM3U, URL: q.URL + "/list.m3u"}}
		if mutate != nil {
			mutate(c)
		}
	})
}

func TestM3UInputAsTheOnlySource(t *testing.T) {
	q := newProvider(t)
	base := m3uOnly(t, q, nil)

	// the Xtream API: live channels and their categories, nothing else
	streams := listOf(t, base, "get_live_streams", "stream_id")
	if len(streams) != 3 {
		t.Fatalf("channels: %s", ids(streams))
	}
	var one map[string]any
	for _, s := range streams {
		if s["name"] == "One, the first" {
			one = s
		}
	}
	if one == nil || one["epg_channel_id"] != "" || one["stream_type"] != "live" {
		t.Fatalf("the first channel: %v", one)
	}
	categories := listOf(t, base, "get_live_categories", "category_id")
	if len(categories) != 1 {
		t.Errorf("categories: %v", categories)
	}
	for _, c := range categories {
		if c["category_name"] != "News" || c["category_id"] != one["category_id"] {
			t.Errorf("category %v for a channel of %v", c, one["category_id"])
		}
	}
	if movies := listOf(t, base, "get_vod_streams", "stream_id"); len(movies) != 0 {
		t.Errorf("movies: %v", movies)
	}

	// the stream of a channel is the track's
	id := fmt.Sprint(one["stream_id"])
	for _, path := range []string{"/live/me/secret/" + id + ".ts", "/me/secret/" + id} {
		if resp, body := get(t, base+path); resp.StatusCode != http.StatusOK || body != "track-a" {
			t.Errorf("%s: status %d, %q", path, resp.StatusCode, body)
		}
	}
	if resp, _ := get(t, base+"/live/me/secret/12345.ts"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("an unknown channel: status %d", resp.StatusCode)
	}

	// the login answer is the proxy's
	resp, login := get(t, base+"/player_api.php?"+creds)
	if resp.StatusCode != http.StatusOK || !strings.Contains(login, `"username":"me"`) || strings.Contains(login, "input.invalid") {
		t.Errorf("login: status %d: %s", resp.StatusCode, login)
	}

	// the playlist is made of the channels, at the proxy's address
	resp, playlist := get(t, base+"/iptv.m3u?"+creds+"&output=ts")
	if resp.StatusCode != http.StatusOK || !strings.Contains(playlist, "http://proxy.example:8080/live/me/secret/"+id+".ts") ||
		strings.Contains(playlist, "input.invalid") || strings.Contains(playlist, q.URL) {
		t.Errorf("playlist: status %d:\n%s", resp.StatusCode, playlist)
	}

	// the guide is the one the playlist names
	if resp, guide := get(t, base+"/xmltv.php?"+creds); resp.StatusCode != http.StatusOK || guide != providerGuide {
		t.Errorf("guide: status %d:\n%s", resp.StatusCode, guide)
	}
}

func TestM3UInputGuideOfItsOwn(t *testing.T) {
	q := newProvider(t)
	base := m3uOnly(t, q, func(c *config.ProxyConfig) { c.Sources[0].XMLTVURL = q.URL + "/leaky-guide-not-there" })
	if resp, _ := get(t, base+"/xmltv.php?"+creds); resp.StatusCode == http.StatusOK {
		t.Error("the guide of the settings is not the one asked for")
	}
}

func TestM3UInputAmongXtreamSources(t *testing.T) {
	p, q := newProvider(t), newProvider(t)

	for name, build := range map[string]func(*config.ProxyConfig){
		"the Xtream account first": func(c *config.ProxyConfig) {
			c.Sources = []config.Source{{Name: "portugal", Type: config.SourceM3U, URL: q.URL + "/list.m3u"}}
		},
		"the playlist first": func(c *config.ProxyConfig) {
			c.XtreamBaseURL, c.XtreamUser, c.XtreamPassword = "", "", ""
			c.Sources = []config.Source{
				{Name: "portugal", Type: config.SourceM3U, URL: q.URL + "/list.m3u"},
				{Name: "brasil", XtreamBaseURL: p.URL, XtreamUser: xUser, XtreamPassword: xPass},
			}
		},
	} {
		before := q.count("/stream/a.ts")
		base := proxy(t, p, build)

		live := listOf(t, base, "get_live_streams", "stream_id")
		var fromPlaylist, fromAccount []string
		for id, s := range live {
			if s["stream_type"] == "live" && s["name"] != nil && strings.HasPrefix(fmt.Sprint(s["name"]), "One, the first") {
				fromPlaylist = append(fromPlaylist, id)
			} else {
				fromAccount = append(fromAccount, id)
			}
		}
		if len(fromPlaylist) != 1 || len(fromAccount) == 0 {
			t.Fatalf("%s: channels: %s", name, ids(live))
		}

		// the playlist's channels are in the range of their source, and play
		id := fromPlaylist[0]
		n, _ := strconv.ParseInt(id, 10, 64)
		wantRange := slices.Contains([]int64{0, 1}, n/sourceIDs)
		if !wantRange {
			t.Errorf("%s: id %s is in no source's range", name, id)
		}
		if resp, body := get(t, base+"/live/me/secret/"+id+".ts"); resp.StatusCode != http.StatusOK || body != "track-a" {
			t.Errorf("%s: stream: status %d, %q", name, resp.StatusCode, body)
		}
		if q.count("/stream/a.ts") != before+1 {
			t.Errorf("%s: the playlist's track was asked %d times", name, q.count("/stream/a.ts")-before)
		}

		// and the account's channels play too
		for id := range live {
			if id != fromPlaylist[0] {
				if resp, _ := get(t, base+"/live/me/secret/"+id+".ts"); resp.StatusCode >= 500 {
					t.Errorf("%s: channel %s: status %d", name, id, resp.StatusCode)
				}
				break
			}
		}
	}
}
