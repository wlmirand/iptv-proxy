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
	"net/http/httptest"
	"os"
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

func categoryNames(t *testing.T, base string) map[string]string {
	t.Helper()
	names := map[string]string{} // by name, the id
	for id, c := range listOf(t, base, "get_live_categories", "category_id") {
		names[fmt.Sprint(c["category_name"])] = id
	}
	return names
}

func TestCategoryPrefixes(t *testing.T) {
	p, q := newProvider(t), newProvider(t)
	withPrefixes := func(separator string) func(*config.ProxyConfig) {
		return func(c *config.ProxyConfig) {
			c.XtreamCategoryPrefix = " BR "
			c.Sources = []config.Source{{Name: "portugal", Type: config.SourceM3U, URL: q.URL + "/list.m3u", CategoryPrefix: "PT"}}
			c.Output = &config.Output{Formats: []string{config.OutputXtream, config.OutputM3U}, CategorySeparator: separator}
		}
	}

	// without prefixes, categories of the same name are one
	plain := proxy(t, p, func(c *config.ProxyConfig) {
		c.Sources = []config.Source{{Name: "portugal", Type: config.SourceM3U, URL: q.URL + "/list.m3u"}}
	})
	if names := categoryNames(t, plain); names["News"] == "" || len(names) != 2 {
		t.Errorf("no prefixes: %v", names)
	}

	base := proxy(t, p, withPrefixes(""))
	names := categoryNames(t, base)
	for _, want := range []string{"BR | News", "BR | Sport \"HD\"", "PT | News", "PT"} {
		if names[want] == "" {
			t.Errorf("category %q missing from %v", want, names)
		}
	}
	if names["News"] != "" {
		t.Errorf("a category without its prefix: %v", names)
	}

	// the playlist's channels are in their categories: the group has the
	// prefix, and what had none is in the category of the prefix alone
	live := listOf(t, base, "get_live_streams", "stream_id")
	for id, s := range live {
		if n, _ := strconv.ParseInt(id, 10, 64); n < sourceIDs {
			continue // the account's
		}
		name, category := fmt.Sprint(s["name"]), fmt.Sprint(s["category_id"])
		switch {
		case strings.HasPrefix(name, "One, the first") && category != names["PT | News"],
			(name == "Two" || name == "Three") && category != names["PT"]:
			t.Errorf("channel %q is in category %s, categories %v", name, category, names)
		}
	}
	// a category asked for holds what it should
	resp, body := get(t, base+"/player_api.php?"+creds+"&action=get_live_streams&category_id="+names["PT"])
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"Three"`) || strings.Contains(body, "One, the first") {
		t.Errorf("category PT: status %d: %s", resp.StatusCode, body)
	}

	// the playlist's groups are the same
	_, playlist := get(t, base+"/iptv.m3u?"+creds)
	if !strings.Contains(playlist, `group-title="PT | News"`) || !strings.Contains(playlist, `group-title="PT"`) || !strings.Contains(playlist, `group-title="BR | News"`) {
		t.Errorf("playlist:\n%s", playlist)
	}

	// the separator is the one of the settings
	if names := categoryNames(t, proxy(t, p, withPrefixes("/"))); names["BR/News"] == "" || names["PT/News"] == "" || names["PT"] == "" {
		t.Errorf("separator /: %v", names)
	}
}

func TestIndividualViews(t *testing.T) {
	p, q := newProvider(t), newProvider(t)
	prefixed := func(c *config.ProxyConfig) {
		c.XtreamPath, c.XtreamIndividual, c.XtreamCategoryPrefix = "brasil", true, "BR"
		c.Sources = []config.Source{{Name: "portugal", Type: config.SourceM3U, URL: q.URL + "/list.m3u", Path: "portugal", Individual: true, CategoryPrefix: "PT"}}
	}
	base := proxy(t, p, prefixed)
	alone := proxy(t, p, nil)

	// the account's view is the account alone, as it is
	aloneList := listOf(t, alone, "get_live_streams", "stream_id")
	want := ids(aloneList)
	combined := listOf(t, base, "get_live_streams", "stream_id")
	for id := range aloneList {
		if combined[id] == nil {
			t.Errorf("combined: %s lacks %s of the account alone", ids(combined), id)
		}
	}
	if len(combined) <= len(aloneList) {
		t.Errorf("combined: %s, the account alone: %s", ids(combined), want)
	}
	if got := ids(listOf(t, base+"/brasil", "get_live_streams", "stream_id")); got != want {
		t.Errorf("/brasil: %s, want %s", got, want)
	}
	if got := categoryNames(t, base+"/brasil"); got["News"] == "" || got["BR | News"] != "" || len(got) != 2 {
		t.Errorf("/brasil categories: %v", got)
	}

	// the playlist's view: its ids, its categories, no prefix
	view := listOf(t, base+"/portugal", "get_live_streams", "stream_id")
	if len(view) != 3 {
		t.Fatalf("/portugal channels: %s", ids(view))
	}
	var id string
	for k, s := range view {
		if n, _ := strconv.ParseInt(k, 10, 64); n >= sourceIDs {
			t.Errorf("/portugal shows %s: a range of the combined catalogue", k)
		}
		if strings.HasPrefix(fmt.Sprint(s["name"]), "One, the first") {
			id = k
		}
	}
	if got := categoryNames(t, base+"/portugal"); got["News"] == "" || len(got) != 1 {
		t.Errorf("/portugal categories: %v", got)
	}
	if got := categoryNames(t, base); got["PT | News"] == "" || got["News"] != "" {
		t.Errorf("combined categories: %v", got)
	}

	// it plays, at its own address
	if resp, body := get(t, base+"/portugal/live/me/secret/"+id+".ts"); resp.StatusCode != http.StatusOK || body != "track-a" {
		t.Errorf("/portugal stream: status %d, %q", resp.StatusCode, body)
	}
	resp, playlist := get(t, base+"/portugal/iptv.m3u?"+creds+"&output=ts")
	if resp.StatusCode != http.StatusOK || !strings.Contains(playlist, "http://proxy.example:8080/portugal/live/me/secret/"+id+".ts") ||
		strings.Contains(playlist, "PT") || strings.Contains(playlist, "input.invalid") {
		t.Errorf("/portugal playlist: status %d:\n%s", resp.StatusCode, playlist)
	}
	if resp, guide := get(t, base+"/portugal/xmltv.php?"+creds); resp.StatusCode != http.StatusOK || guide != providerGuide {
		t.Errorf("/portugal guide: status %d", resp.StatusCode)
	}

	// a view plays what is its own: an id of another source is not there
	other := ""
	for k := range listOf(t, base+"/brasil", "get_live_streams", "stream_id") {
		if _, own := view[k]; !own {
			other = k
			break
		}
	}
	if resp, _ := get(t, base+"/portugal/live/me/secret/"+other+".ts"); resp.StatusCode < 400 {
		t.Errorf("an id of another source plays at /portugal: status %d", resp.StatusCode)
	}

	// a user keeps their login, and a wrong one is refused
	if resp, _ := get(t, base+"/portugal/player_api.php?username=me&password=nope"); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong password: status %d", resp.StatusCode)
	}
}

func TestOnlyViews(t *testing.T) {
	p, q := newProvider(t), newProvider(t)
	base := proxy(t, p, func(c *config.ProxyConfig) {
		c.XtreamPath, c.XtreamIndividual, c.NoCombined = "brasil", true, true
		c.Sources = []config.Source{{Name: "portugal", Type: config.SourceM3U, URL: q.URL + "/list.m3u", Path: "portugal", Individual: true}}
	})
	for path, want := range map[string]bool{
		"/player_api.php?" + creds:                     false,
		"/iptv.m3u?" + creds:                           false,
		"/brasil/player_api.php?" + creds:              true,
		"/portugal/player_api.php?" + creds:            true,
		"/portugal/iptv.m3u?" + creds:                  true,
		"/live/me/secret/1.ts":                         false,
		"/brasil/live/me/secret/1.ts":                  true,
		"/portugal/xmltv.php?" + creds:                 true,
		"/brasil/player_api.php?username=x&password=y": false,
	} {
		got := codeOf(t, base, path)
		if (got != http.StatusNotFound && got != http.StatusUnauthorized) != want {
			t.Errorf("%s: status %d, served: %v", path, got, want)
		}
	}
}

func TestViewsShareWhatIsTheAccounts(t *testing.T) {
	p, q := newProvider(t), newProvider(t)
	conf := &config.ProxyConfig{
		HostConfig: &config.HostConfiguration{Hostname: "proxy.example", Port: 8080}, XtreamBaseURL: p.URL,
		XtreamUser: xUser, XtreamPassword: xPass, User: user, Password: pass, M3UFileName: "iptv.m3u", M3UCacheExpiration: 1,
		XtreamPath: "brasil", XtreamIndividual: true,
		Sources: []config.Source{{Name: "portugal", Type: config.SourceM3U, URL: q.URL + "/list.m3u"}},
	}
	c, err := NewServer(conf)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.views) != 1 {
		t.Fatalf("%d views", len(c.views))
	}
	v := c.views[0]
	if v.sources[0].usage != c.sources[0].usage || v.hub != c.hub || v.users[0].slots != c.users[0].slots {
		t.Error("a view does not share the connections of the combined catalogue")
	}
	if v.sources[0].base != 0 || v.users[0].provider != c.sources[0].account || v.CustomEndpoint != "brasil" {
		t.Errorf("view: base %d, account %v, endpoint %q", v.sources[0].base, v.users[0].provider, v.CustomEndpoint)
	}
}

func TestViewPathsAreChecked(t *testing.T) {
	p, q := newProvider(t), newProvider(t)
	for name, c := range map[string]struct{ first, second string }{
		"a user's name":   {"me", "pt"},
		"a proxy path":    {"live", "pt"},
		"the same twice":  {"br", "br"},
		"not an address":  {"b r", "pt"},
		"the playlist's":  {"iptv.m3u", "pt"},
		"the status page": {"status", "pt"},
	} {
		conf := &config.ProxyConfig{
			HostConfig: &config.HostConfiguration{Hostname: "proxy.example", Port: 8080}, XtreamBaseURL: p.URL,
			XtreamUser: xUser, XtreamPassword: xPass, User: user, Password: pass, M3UFileName: "iptv.m3u", M3UCacheExpiration: 1,
			XtreamPath: c.first, XtreamIndividual: true,
			Sources: []config.Source{{Name: "portugal", Type: config.SourceM3U, URL: q.URL + "/list.m3u", Path: c.second, Individual: true}},
		}
		if _, err := NewServer(conf); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// moviesPlaylist is a playlist with movies, as a provider writes them, and
// the server that has them.
func moviesPlaylist(t *testing.T) (list string, films *httptest.Server) {
	t.Helper()
	films = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "film:"+r.URL.Path)
	}))
	t.Cleanup(films.Close)

	list = t.TempDir() + "/movies.m3u"
	content := "#EXTM3U\n" +
		"#EXTINF:-1 group-title=\"Filmes | Action\" tvg-logo=\"http://logos.example/dh.jpg\",Die Hard\n" + films.URL + "/films/die-hard.mkv\n" +
		"#EXTINF:-1 group-title=\"Filmes | Drama\",Titanic\n" + films.URL + "/films/titanic.mp4\n" +
		"#EXTINF:-1,Nameless\n" + films.URL + "/films/nameless.avi\n" +
		"#EXTINF:-1 group-title=\"TV\",Live One\n" + films.URL + "/live/one.ts\n"
	if err := os.WriteFile(list, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return list, films
}

func TestM3UInputMovies(t *testing.T) {
	list, _ := moviesPlaylist(t)
	q := newProvider(t)
	base := m3uOnly(t, q, func(c *config.ProxyConfig) { c.Sources[0].URL = list })

	// movies are movies, and the live channels stay live
	movies := listOf(t, base, "get_vod_streams", "stream_id")
	if len(movies) != 3 {
		t.Fatalf("movies: %s", ids(movies))
	}
	extensions := map[string]string{}
	var hard string
	for id, m := range movies {
		extensions[fmt.Sprint(m["name"])] = fmt.Sprint(m["container_extension"])
		if m["name"] == "Die Hard" {
			hard = id
		}
		if m["stream_type"] != "movie" {
			t.Errorf("%v is not a movie", m)
		}
	}
	if extensions["Die Hard"] != "mkv" || extensions["Titanic"] != "mp4" || extensions["Nameless"] != "avi" {
		t.Errorf("containers: %v", extensions)
	}
	if live := listOf(t, base, "get_live_streams", "stream_id"); len(live) != 1 {
		t.Errorf("live channels: %s", ids(live))
	}
	names := map[string]bool{}
	for _, c := range listOf(t, base, "get_vod_categories", "category_id") {
		names[fmt.Sprint(c["category_name"])] = true
	}
	if !names["Filmes | Action"] || !names["Filmes | Drama"] || len(names) != 2 {
		t.Errorf("movie categories: %v", names)
	}

	// a movie plays, and has its details
	if resp, body := get(t, base+"/movie/me/secret/"+hard+".mkv"); resp.StatusCode != http.StatusOK || body != "film:/films/die-hard.mkv" {
		t.Errorf("movie: status %d, %q", resp.StatusCode, body)
	}
	if _, body := get(t, base+"/player_api.php?"+creds+"&action=get_vod_info&vod_id="+hard); !strings.Contains(body, `"container_extension":"mkv"`) || !strings.Contains(body, "Die Hard") {
		t.Errorf("movie details: %s", body)
	}

	// the playlist has them all, with the category they had, with no setting
	_, playlist := get(t, base+"/iptv.m3u?"+creds)
	for _, want := range []string{`group-title="Filmes | Action",Die Hard`, `group-title="Filmes | Drama",Titanic`, ",Nameless", `group-title="TV",Live One`, "/movie/me/secret/" + hard + ".mkv"} {
		if !strings.Contains(playlist, want) {
			t.Errorf("playlist lacks %q:\n%s", want, playlist)
		}
	}
	if strings.Contains(playlist, "input.invalid") {
		t.Errorf("playlist:\n%s", playlist)
	}
}

func TestM3UInputMoviesAmongSources(t *testing.T) {
	list, _ := moviesPlaylist(t)
	p := newProvider(t)
	base := proxy(t, p, func(c *config.ProxyConfig) {
		c.Sources = []config.Source{{Name: "portugal", Type: config.SourceM3U, URL: list, CategoryPrefix: "PT"}}
	})

	names := map[string]bool{}
	for _, c := range listOf(t, base, "get_vod_categories", "category_id") {
		names[fmt.Sprint(c["category_name"])] = true
	}
	for _, want := range []string{"PT | Filmes | Action", "PT | Filmes | Drama", "PT"} {
		if !names[want] {
			t.Errorf("category %q missing from %v", want, names)
		}
	}

	// the account's movies are not in the playlist unless asked; the
	// playlist's are
	_, playlist := get(t, base+"/iptv.m3u?"+creds)
	if !strings.Contains(playlist, `group-title="PT | Filmes | Action",Die Hard`) || !strings.Contains(playlist, `group-title="PT",Nameless`) {
		t.Errorf("playlist:\n%s", playlist)
	}
	for id, m := range listOf(t, base, "get_vod_streams", "stream_id") {
		if n, _ := strconv.ParseInt(id, 10, 64); n < sourceIDs && strings.Contains(playlist, fmt.Sprint(m["name"])+"\n") {
			t.Errorf("a movie of the account is in the playlist: %v", m["name"])
		}
	}
	// and they play, from the range of their source
	for id, m := range listOf(t, base, "get_vod_streams", "stream_id") {
		if m["name"] == "Die Hard" {
			if resp, body := get(t, base+"/movie/me/secret/"+id+".mkv"); resp.StatusCode != http.StatusOK || body != "film:/films/die-hard.mkv" {
				t.Errorf("movie %s: status %d, %q", id, resp.StatusCode, body)
			}
		}
	}
}

// The same address in two groups is two entries, one in each category; the
// same address in the same group is one.
func TestM3UInputAddressInTwoGroups(t *testing.T) {
	films := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "film:"+r.URL.Path) }))
	t.Cleanup(films.Close)
	list := t.TempDir() + "/list.m3u"
	content := "#EXTM3U\n" +
		"#EXTINF:-1 group-title=\"Filmes | Action\",Die Hard\n" + films.URL + "/f/dh.mkv\n" +
		"#EXTINF:-1 group-title=\"Filmes | Lancamentos\",Die Hard\n" + films.URL + "/f/dh.mkv\n" +
		"#EXTINF:-1 group-title=\"Filmes | Lancamentos\",Die Hard\n" + films.URL + "/f/dh.mkv\n" +
		"#EXTINF:-1 group-title=\"Canais | News\",News\n" + films.URL + "/live/news.ts\n" +
		"#EXTINF:-1 group-title=\"Canais | Favoritos\",News\n" + films.URL + "/live/news.ts\n"
	if err := os.WriteFile(list, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	base := m3uOnly(t, newProvider(t), func(c *config.ProxyConfig) { c.Sources[0].URL = list })

	movies := listOf(t, base, "get_vod_streams", "stream_id")
	live := listOf(t, base, "get_live_streams", "stream_id")
	if len(movies) != 2 || len(live) != 2 {
		t.Fatalf("movies %s, channels %s", ids(movies), ids(live))
	}
	categories := map[string]bool{}
	for id, m := range movies {
		categories[fmt.Sprint(m["category_id"])] = true
		// both entries play the same file
		if resp, body := get(t, base+"/movie/me/secret/"+id+".mkv"); resp.StatusCode != http.StatusOK || body != "film:/f/dh.mkv" {
			t.Errorf("movie %s: status %d, %q", id, resp.StatusCode, body)
		}
	}
	if len(categories) != 2 {
		t.Errorf("the movie is in %d categories", len(categories))
	}

	_, playlist := get(t, base+"/iptv.m3u?"+creds)
	if n := strings.Count(playlist, "#EXTINF"); n != 4 {
		t.Errorf("%d entries in the playlist:\n%s", n, playlist)
	}
}
