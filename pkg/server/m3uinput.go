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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pierre-emmanuelJ/iptv-proxy/pkg/config"
	"github.com/pierre-emmanuelJ/iptv-proxy/pkg/m3u"
	"github.com/pierre-emmanuelJ/iptv-proxy/pkg/xtream"
)

// An M3U playlist is a source like the others: the proxy serves it to
// itself as an Xtream account of its own, whose live channels are the
// playlist's tracks. Merging, failover, connection limits, guides and
// the HDHomeRun tuner then treat it as any account, and what the proxy
// delivers (Xtream, M3U, tuner) can come from it.
//
// The account has no network address: its host is made up, and requests to
// it are answered in memory (see inputTransport). A stream is a redirect to
// the track's address, which the proxy follows itself: no client sees it.
//
// A track gets a numeric id, as an Xtream channel has one. The id comes from
// the track's address, so that a channel keeps its id when the playlist
// changes, wherever it moves in it. Two addresses with the same id are told
// apart by the order of the addresses: the first keeps it, the other takes
// the next one that is free. Only tracks that collide are affected by a
// change of the playlist.
//
// A track with the address of a file (a movie: .mp4, .mkv...) is a movie of
// the account, with the container its extension says; any other track is a
// live channel. A playlist says nothing of seasons: it has no series.

// m3uInputHost is the end of the made up hosts of the M3U accounts. ".invalid"
// is reserved: such a name never resolves.
const m3uInputHost = ".input.invalid"

type m3uInput struct {
	name string
	// host is the made up host of the account.
	host    string
	address string
	// guide is the guide given in the settings; else the playlist's.
	guide   string
	max     int
	account xtream.Account

	mu        sync.Mutex
	catalogue *m3uCatalogue
	at        time.Time
}

// m3uCatalogue is what is read of a playlist, ready to be served.
type m3uCatalogue struct {
	categories []byte // the Xtream lists, as JSON
	streams    []byte
	// the same for movies
	movieCategories []byte
	movies          []byte
	// details of a movie, by its id
	details map[string]movieDetails
	// addresses are the tracks' addresses, by stream id.
	addresses map[string]string
	guide     string
	// uncategorized tell that some channels, some movies have no group.
	uncategorized, uncategorizedMovies bool
	// what it holds, for the logs
	channels, films, groups int
}

type movieDetails struct {
	name, icon, extension string
}

func newM3UInput(position int, s config.Source) *m3uInput {
	host := "m3u-" + strconv.Itoa(position+1) + m3uInputHost
	return &m3uInput{
		name:    s.Name,
		host:    host,
		address: s.URL,
		guide:   s.XMLTVURL,
		max:     s.MaxConnections,
		account: xtream.Account{BaseURL: "http://" + host, User: "m3u", Password: "m3u"},
	}
}

// hashID is the id an address would have: a number from 1 to sourceIDs-1.
func hashID(key string) int64 {
	sum := sha256.Sum256([]byte(key))
	return int64(binary.BigEndian.Uint64(sum[:8])%(sourceIDs-1)) + 1
}

// assignIDs gives each key an id of its own, from the key itself.
func assignIDs(keys []string) map[string]int64 {
	sorted := slices.Clone(keys)
	slices.Sort(sorted)
	sorted = slices.Compact(sorted)

	ids := make(map[string]int64, len(sorted))
	taken := make(map[int64]bool, len(sorted))
	var displaced []string
	for _, key := range sorted {
		id := hashID(key)
		if taken[id] {
			displaced = append(displaced, key)
			continue
		}
		taken[id] = true
		ids[key] = id
	}
	// They come after every key that has its own id: whoever collides
	// moves, whoever does not stays.
	for _, key := range displaced {
		id := hashID(key)
		for taken[id] {
			id = id%(sourceIDs-1) + 1
		}
		taken[id] = true
		ids[key] = id
	}
	if len(displaced) > 0 {
		log.Printf("[iptv-proxy] %d tracks of a playlist share an id with another: they take the next one", len(displaced))
	}
	return ids
}

// playlistGuide is the guide a playlist names in its header.
func playlistGuide(header string) string {
	for _, name := range []string{"url-tvg", "x-tvg-url"} {
		if value, ok := m3u.Attribute(header, name); ok {
			// several guides may be listed: the first one is served
			if first := strings.TrimSpace(strings.Split(value, ",")[0]); first != "" {
				return first
			}
		}
	}
	return ""
}

// trackID tells a track from another: its address and its group. The same
// address in two groups (a movie in two categories) is two entries.
func trackID(track m3u.Track) string {
	return track.URI + "\x00" + track.Group()
}

// tracksOf are the tracks of a playlist that have an address: the live
// channels, and the movies (those whose address is a file). A track that
// repeats another (the same address in the same group) is left out.
func tracksOf(playlist *m3u.Playlist) (live, movies []m3u.Track) {
	seen := map[string]bool{}
	dropped := 0
	for _, track := range playlist.Tracks {
		address, err := url.Parse(track.URI)
		if err != nil || track.URI == "" {
			dropped++
			continue
		}
		if seen[trackID(track)] {
			continue
		}
		seen[trackID(track)] = true
		if fileExtensions[strings.ToLower(path.Ext(address.Path))] {
			movies = append(movies, track)
		} else {
			live = append(live, track)
		}
	}
	if dropped > 0 {
		log.Printf("[iptv-proxy] %d tracks of a playlist have no usable address: left out", dropped)
	}
	return live, movies
}

type xtreamCategory struct {
	ID       string `json:"category_id"`
	Name     string `json:"category_name"`
	ParentID int    `json:"parent_id"`
}

func newM3UCatalogue(playlist *m3u.Playlist) (*m3uCatalogue, error) {
	live, movies := tracksOf(playlist)

	// ids are of the addresses, and of the groups: both lists are asked for
	// separately, but a group of the same name has the same id in both
	var addresses, groups []string
	liveGroups, movieGroups := map[string]bool{}, map[string]bool{}
	var liveOrder, movieOrder []string
	for _, tracks := range [][]m3u.Track{live, movies} {
		for _, track := range tracks {
			addresses = append(addresses, trackID(track))
			g := track.Group()
			if g == "" {
				continue
			}
			groups = append(groups, g)
			seen, order := &liveGroups, &liveOrder
			if fileExtensions[strings.ToLower(path.Ext(pathOf(track.URI)))] {
				seen, order = &movieGroups, &movieOrder
			}
			if !(*seen)[g] {
				(*seen)[g] = true
				*order = append(*order, g)
			}
		}
	}
	streamIDs := assignIDs(addresses)
	categoryIDs := assignIDs(groups)

	type stream struct {
		Num          int    `json:"num"`
		Name         string `json:"name"`
		StreamType   string `json:"stream_type"`
		StreamID     int64  `json:"stream_id"`
		StreamIcon   string `json:"stream_icon"`
		EPGChannelID string `json:"epg_channel_id"`
		Added        string `json:"added"`
		CategoryID   string `json:"category_id"`
		CustomSID    string `json:"custom_sid"`
		TVArchive    int    `json:"tv_archive"`
		DirectSource string `json:"direct_source"`
	}
	type movie struct {
		Num                int    `json:"num"`
		Name               string `json:"name"`
		StreamType         string `json:"stream_type"`
		StreamID           int64  `json:"stream_id"`
		StreamIcon         string `json:"stream_icon"`
		Rating             string `json:"rating"`
		Rating5            int    `json:"rating_5based"`
		Added              string `json:"added"`
		CategoryID         string `json:"category_id"`
		ContainerExtension string `json:"container_extension"`
		CustomSID          string `json:"custom_sid"`
		DirectSource       string `json:"direct_source"`
	}

	cat := &m3uCatalogue{addresses: make(map[string]string, len(addresses)), details: map[string]movieDetails{}, guide: playlistGuide(playlist.Header)}
	categoriesOf := func(order []string) []xtreamCategory {
		list := make([]xtreamCategory, 0, len(order))
		for _, g := range order {
			list = append(list, xtreamCategory{ID: strconv.FormatInt(categoryIDs[g], 10), Name: g})
		}
		return list
	}
	nameOf := func(track m3u.Track) string {
		if name := track.Name(); name != "" {
			return name
		}
		name, _ := m3u.Attribute(track.ExtInf, "tvg-name")
		return name
	}
	categoryOf := func(track m3u.Track, uncategorized *bool) string {
		if g := track.Group(); g != "" {
			return strconv.FormatInt(categoryIDs[g], 10)
		}
		*uncategorized = true
		return ""
	}

	streams := make([]stream, 0, len(live))
	for i, track := range live {
		logo, _ := m3u.Attribute(track.ExtInf, "tvg-logo")
		guide, _ := m3u.Attribute(track.ExtInf, "tvg-id")
		id := streamIDs[trackID(track)]
		cat.addresses[strconv.FormatInt(id, 10)] = track.URI
		streams = append(streams, stream{
			Num: i + 1, Name: nameOf(track), StreamType: "live", StreamID: id, StreamIcon: logo,
			EPGChannelID: guide, Added: "0", CategoryID: categoryOf(track, &cat.uncategorized),
		})
	}
	films := make([]movie, 0, len(movies))
	for i, track := range movies {
		logo, _ := m3u.Attribute(track.ExtInf, "tvg-logo")
		extension := strings.TrimPrefix(strings.ToLower(path.Ext(pathOf(track.URI))), ".")
		id := streamIDs[trackID(track)]
		key := strconv.FormatInt(id, 10)
		cat.addresses[key] = track.URI
		cat.details[key] = movieDetails{name: nameOf(track), icon: logo, extension: extension}
		films = append(films, movie{
			Num: i + 1, Name: nameOf(track), StreamType: "movie", StreamID: id, StreamIcon: logo,
			Rating: "0", Added: "0", CategoryID: categoryOf(track, &cat.uncategorizedMovies), ContainerExtension: extension,
		})
	}

	cat.channels, cat.films = len(live), len(movies)
	cat.groups = len(liveOrder) + len(movieOrder)

	var err error
	for _, out := range []struct {
		to *[]byte
		v  any
	}{
		{&cat.categories, categoriesOf(liveOrder)}, {&cat.streams, streams},
		{&cat.movieCategories, categoriesOf(movieOrder)}, {&cat.movies, films},
	} {
		if *out.to, err = marshal(out.v); err != nil {
			return nil, err
		}
	}
	return cat, nil
}

// pathOf is the path of an address, "" if it is not one.
func pathOf(address string) string {
	u, err := url.Parse(address)
	if err != nil {
		return ""
	}
	return u.Path
}

// marshal writes JSON the way the provider's lists are: addresses and
// markup untouched.
func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// current returns what the playlist holds, reading it again once it is older
// than the playlist cache. A playlist that cannot be read again is kept.
func (c *Config) current(ctx context.Context, in *m3uInput, userAgent string) (*m3uCatalogue, error) {
	in.mu.Lock()
	defer in.mu.Unlock()
	expiration := time.Duration(c.M3UCacheExpiration) * time.Hour
	if in.catalogue != nil && time.Since(in.at) < expiration {
		return in.catalogue, nil
	}

	// Whoever asked first must not leave the others with nothing by leaving.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), playlistFetchTimeout)
	defer cancel()
	playlist, err := c.loadPlaylist(ctx, in.address, userAgent)
	var fresh *m3uCatalogue
	if err == nil {
		fresh, err = newM3UCatalogue(playlist)
	}
	if err != nil {
		if in.catalogue == nil {
			return nil, err
		}
		log.Printf("[iptv-proxy] %s: the playlist could not be read again (%v), serving the last one", in.name, err)
		in.at = time.Now().Add(retryFailedReload - expiration)
		return in.catalogue, nil
	}
	in.catalogue, in.at = fresh, time.Now()
	return fresh, nil
}

// inputTransport answers the requests to the M3U accounts, and sends the
// others where they go.
type inputTransport struct {
	c    *Config
	next http.RoundTripper
}

func (t *inputTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if in := t.c.m3uInputs[req.URL.Hostname()]; in != nil {
		return t.c.serveM3UInput(in, req), nil
	}
	return t.next.RoundTrip(req)
}

func inMemory(req *http.Request, status int, contentType string, body io.ReadCloser, length int64) *http.Response {
	header := http.Header{}
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	return &http.Response{
		Status:        strconv.Itoa(status) + " " + http.StatusText(status),
		StatusCode:    status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        header,
		Body:          body,
		ContentLength: length,
		Request:       req,
	}
}

func inMemoryBytes(req *http.Request, status int, contentType string, body []byte) *http.Response {
	return inMemory(req, status, contentType, io.NopCloser(bytes.NewReader(body)), int64(len(body)))
}

func redirect(req *http.Request, address string) *http.Response {
	resp := inMemoryBytes(req, http.StatusFound, "", nil)
	resp.Header.Set("Location", address)
	return resp
}

// serveM3UInput answers a request to the account of an M3U playlist: the
// Xtream API, the stream of a channel and the guide.
func (c *Config) serveM3UInput(in *m3uInput, req *http.Request) *http.Response {
	parts := strings.Split(strings.Trim(req.URL.Path, "/"), "/")
	notFound := func() *http.Response {
		return inMemoryBytes(req, http.StatusNotFound, "text/plain", []byte("not found"))
	}

	switch {
	case len(parts) == 1 && parts[0] == "player_api.php":
		return c.m3uInputAPI(in, req)
	case len(parts) == 1 && parts[0] == "xmltv.php":
		return c.m3uInputGuide(in, req)
	}

	// "<user>/<password>/<id>", "live/..." and "movie/...": the channels, the movies
	var id string
	switch {
	case len(parts) == 3:
		id = parts[2]
	case len(parts) == 4 && (parts[0] == "live" || parts[0] == "movie"):
		id = parts[3]
	default:
		return notFound()
	}
	cat, err := c.current(req.Context(), in, req.UserAgent())
	if err != nil {
		return inMemoryBytes(req, http.StatusBadGateway, "text/plain", []byte("the playlist could not be read"))
	}
	address, ok := cat.addresses[strings.TrimSuffix(id, path.Ext(id))]
	if !ok {
		return notFound()
	}
	return redirect(req, address)
}

func (c *Config) m3uInputAPI(in *m3uInput, req *http.Request) *http.Response {
	const jsonType = "application/json"
	action := req.URL.Query().Get("action")

	switch action {
	case "":
		login := map[string]any{
			"user_info": map[string]any{
				"username": in.account.User, "password": in.account.Password, "message": "", "auth": 1,
				"status": "Active", "exp_date": nil, "is_trial": "0", "active_cons": "0", "created_at": "0",
				"max_connections":        strconv.Itoa(in.max),
				"allowed_output_formats": []string{"m3u8", "ts"},
			},
			"server_info": map[string]any{
				"url": "", "port": "80", "https_port": "443", "server_protocol": "http", "rtmp_port": "0",
				"timezone": "UTC", "timestamp_now": time.Now().Unix(), "time_now": time.Now().Format("2006-01-02 15:04:05"),
			},
		}
		body, err := marshal(login)
		if err != nil {
			return inMemoryBytes(req, http.StatusInternalServerError, "text/plain", []byte("error"))
		}
		return inMemoryBytes(req, http.StatusOK, jsonType, body)
	case "get_live_categories", "get_live_streams":
		cat, err := c.current(req.Context(), in, req.UserAgent())
		if err != nil {
			return inMemoryBytes(req, http.StatusBadGateway, "text/plain", []byte("the playlist could not be read"))
		}
		if action == "get_live_categories" {
			return inMemoryBytes(req, http.StatusOK, jsonType, cat.categories)
		}
		return inMemoryBytes(req, http.StatusOK, jsonType, cat.streams)
	case "get_vod_categories", "get_vod_streams":
		cat, err := c.current(req.Context(), in, req.UserAgent())
		if err != nil {
			return inMemoryBytes(req, http.StatusBadGateway, "text/plain", []byte("the playlist could not be read"))
		}
		if action == "get_vod_categories" {
			return inMemoryBytes(req, http.StatusOK, jsonType, cat.movieCategories)
		}
		return inMemoryBytes(req, http.StatusOK, jsonType, cat.movies)
	case "get_vod_info":
		cat, err := c.current(req.Context(), in, req.UserAgent())
		if err != nil {
			return inMemoryBytes(req, http.StatusBadGateway, "text/plain", []byte("the playlist could not be read"))
		}
		id := req.URL.Query().Get("vod_id")
		film, ok := cat.details[id]
		if !ok {
			return inMemoryBytes(req, http.StatusOK, jsonType, []byte(`{"info":[],"movie_data":[]}`))
		}
		n, _ := strconv.ParseInt(id, 10, 64)
		body, err := marshal(map[string]any{
			"info": map[string]any{"name": film.name, "movie_image": film.icon},
			"movie_data": map[string]any{
				"stream_id": n, "name": film.name, "added": "0", "container_extension": film.extension,
			},
		})
		if err != nil {
			return inMemoryBytes(req, http.StatusInternalServerError, "text/plain", []byte("error"))
		}
		return inMemoryBytes(req, http.StatusOK, jsonType, body)
	case "get_short_epg", "get_simple_data_table":
		return inMemoryBytes(req, http.StatusOK, jsonType, []byte(`{"epg_listings":[]}`))
	default:
		// series: a playlist has none
		return inMemoryBytes(req, http.StatusOK, jsonType, []byte("[]"))
	}
}

func (c *Config) m3uInputGuide(in *m3uInput, req *http.Request) *http.Response {
	address := in.guide
	if address == "" {
		cat, err := c.current(req.Context(), in, req.UserAgent())
		if err != nil {
			return inMemoryBytes(req, http.StatusBadGateway, "text/plain", []byte("the playlist could not be read"))
		}
		address = cat.guide
	}
	switch {
	case address == "":
		return inMemoryBytes(req, http.StatusNotFound, "text/plain", []byte("no guide"))
	case strings.HasPrefix(address, "http://") || strings.HasPrefix(address, "https://"):
		return redirect(req, address)
	}
	f, err := os.Open(address)
	if err != nil {
		return inMemoryBytes(req, http.StatusNotFound, "text/plain", []byte(fmt.Sprintf("%s: the guide cannot be opened", in.name)))
	}
	return inMemory(req, http.StatusOK, "application/xml", f, -1)
}

// preload reads the playlists of the M3U inputs, all at once, as the proxy
// starts: the first client does not wait for them, and the log says how it
// goes. A client that comes while they are read waits for the same reading.
func (c *Config) preload() {
	var inputs []*m3uInput
	for _, src := range c.sources {
		if src.m3u != nil {
			inputs = append(inputs, src.m3u)
		}
	}
	started := time.Now()
	for _, in := range inputs {
		log.Printf("[iptv-proxy] Reading %q (m3u, %s)...", in.name, hostOf(in.address))
	}

	var wg sync.WaitGroup
	failed := 0
	var mu sync.Mutex
	for _, in := range inputs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			begun := time.Now()
			cat, err := c.current(context.Background(), in, c.upstreamUserAgent(""))
			if err != nil {
				mu.Lock()
				failed++
				mu.Unlock()
				log.Printf("[iptv-proxy] %q failed after %s: %v: it is read again when a client asks", in.name, since(begun), err)
				return
			}
			log.Printf("[iptv-proxy] %q ready: %d channels, %d movies, %d categories (%s)", in.name, cat.channels, cat.films, cat.groups, since(begun))
		}()
	}
	wg.Wait()

	switch {
	case len(inputs) == 0:
		log.Printf("[iptv-proxy] Ready")
	case failed > 0:
		log.Printf("[iptv-proxy] Ready, with %d of %d playlist(s) not read (%s)", failed, len(inputs), since(started))
	default:
		log.Printf("[iptv-proxy] Ready: %d playlist(s) read in %s", len(inputs), since(started))
	}
}

// since is the time elapsed, rounded for a log.
func since(t time.Time) time.Duration {
	d := time.Since(t)
	if d < time.Second {
		return d.Round(time.Millisecond)
	}
	return d.Round(100 * time.Millisecond)
}
