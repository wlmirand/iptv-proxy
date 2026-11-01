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
// Only live channels are listed: a track with the address of a file (a
// movie, a song) is left out, and the account has no movies or series.

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
	categories []byte // the Xtream list, as JSON
	streams    []byte
	// addresses are the tracks' addresses, by stream id.
	addresses map[string]string
	guide     string
	// uncategorized tells that some channels have no group.
	uncategorized bool
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

// liveTracks are the tracks of a playlist that are live channels, an address
// once.
func liveTracks(playlist *m3u.Playlist) []m3u.Track {
	var live []m3u.Track
	seen := map[string]bool{}
	for _, track := range playlist.Tracks {
		address, err := url.Parse(track.URI)
		if err != nil || track.URI == "" || seen[track.URI] || fileExtensions[strings.ToLower(path.Ext(address.Path))] {
			continue
		}
		seen[track.URI] = true
		live = append(live, track)
	}
	return live
}

func newM3UCatalogue(playlist *m3u.Playlist) (*m3uCatalogue, error) {
	tracks := liveTracks(playlist)

	addresses := make([]string, len(tracks))
	var groups []string
	seenGroup := map[string]bool{}
	for i, track := range tracks {
		addresses[i] = track.URI
		if g := track.Group(); g != "" && !seenGroup[g] {
			seenGroup[g] = true
			groups = append(groups, g)
		}
	}
	streamIDs := assignIDs(addresses)
	categoryIDs := assignIDs(groups)

	type category struct {
		ID       string `json:"category_id"`
		Name     string `json:"category_name"`
		ParentID int    `json:"parent_id"`
	}
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

	cat := &m3uCatalogue{addresses: make(map[string]string, len(tracks)), guide: playlistGuide(playlist.Header)}
	categories := make([]category, 0, len(groups))
	for _, g := range groups {
		categories = append(categories, category{ID: strconv.FormatInt(categoryIDs[g], 10), Name: g})
	}
	streams := make([]stream, 0, len(tracks))
	for i, track := range tracks {
		name := track.Name()
		if name == "" {
			name, _ = m3u.Attribute(track.ExtInf, "tvg-name")
		}
		logo, _ := m3u.Attribute(track.ExtInf, "tvg-logo")
		guide, _ := m3u.Attribute(track.ExtInf, "tvg-id")
		category := ""
		if g := track.Group(); g != "" {
			category = strconv.FormatInt(categoryIDs[g], 10)
		} else {
			cat.uncategorized = true
		}
		id := streamIDs[track.URI]
		cat.addresses[strconv.FormatInt(id, 10)] = track.URI
		streams = append(streams, stream{
			Num: i + 1, Name: name, StreamType: "live", StreamID: id, StreamIcon: logo,
			EPGChannelID: guide, Added: "0", CategoryID: category,
		})
	}

	var err error
	if cat.categories, err = marshal(categories); err != nil {
		return nil, err
	}
	if cat.streams, err = marshal(streams); err != nil {
		return nil, err
	}
	return cat, nil
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

	// "<user>/<password>/<id>" and "live/<user>/<password>/<id>": the channels
	var id string
	switch {
	case len(parts) == 3:
		id = parts[2]
	case len(parts) == 4 && parts[0] == "live":
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
	case "get_short_epg", "get_simple_data_table":
		return inMemoryBytes(req, http.StatusOK, jsonType, []byte(`{"epg_listings":[]}`))
	default:
		// movies and series: a playlist has none
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
