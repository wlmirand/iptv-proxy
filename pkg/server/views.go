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
	"path"
	"regexp"
	"slices"

	"github.com/pierre-emmanuelJ/iptv-proxy/pkg/config"
	"github.com/pierre-emmanuelJ/iptv-proxy/pkg/filter"
)

// A source may also be served on its own, under "/<path>/": the same
// playlist, API and guide as the combined catalogue, with that source's
// channels alone, as they are (its ids, its categories).
//
// A view is a Config of its own with that single source. What is the
// account's is shared with the combined catalogue and the other views: the
// streams the proxy holds at the account, shared live streams, each user's
// streams.

var viewPath = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)

// ownPaths are the first elements of the proxy's own addresses, which a view
// may not take.
var ownPaths = map[string]bool{
	"status": true, "status.json": true, "get.php": true, "apiget": true,
	"player_api.php": true, "xmltv.php": true,
}

func (c *Config) setupViews() error {
	taken := map[string]string{}
	for _, src := range c.sources {
		if !src.individual {
			continue
		}
		switch {
		case !viewPath.MatchString(src.path):
			return fmt.Errorf("%s: path %q is not a name for an address: use letters, digits, - _ . ~", src.name, src.path)
		case reservedNames[src.path] || ownPaths[src.path] || src.path == c.M3UFileName || src.path == c.endpointAntiColision || c.usersByName[src.path] != nil:
			return fmt.Errorf("%s: path %q is one of the proxy's own paths or a user's name", src.name, src.path)
		case taken[src.path] != "":
			return fmt.Errorf("%s and %s: the same path %q", taken[src.path], src.name, src.path)
		}
		taken[src.path] = src.name
		c.views = append(c.views, c.newView(src))
	}
	return nil
}

// newView returns the Config that serves a source alone.
func (c *Config) newView(src *source) *Config {
	conf := *c.ProxyConfig
	conf.CustomEndpoint = path.Join(c.CustomEndpoint, src.path)
	conf.XtreamBaseURL = src.account.BaseURL
	conf.XtreamUser = config.CredentialString(src.account.User)
	conf.XtreamPassword = config.CredentialString(src.account.Password)

	// the source as it is, on its own: no range of ids, no prefix
	own := *src
	own.index, own.base, own.baseID, own.prefix = 0, 0, nil, ""

	users := make([]*proxyUser, len(c.users))
	for i, u := range c.users {
		alone := *u
		alone.provider = src.account
		users[i] = &alone
	}
	return c.derive(conf, []*source{&own}, users, true)
}

// derive returns a Config that serves sources as c does: what is the
// accounts' is shared, the rest (what is remembered of the catalogues, the
// answers kept) is its own.
func (c *Config) derive(conf config.ProxyConfig, sources []*source, users []*proxyUser, view bool) *Config {
	// one status page, one tuner and one set of tuners: the combined
	// catalogue's
	conf.Sources = nil
	conf.NoCombined = false
	conf.StatusPassword = ""
	conf.HDHomeRunPort = 0
	conf.Tuners = nil

	d := &Config{
		ProxyConfig:          &conf,
		endpointAntiColision: c.endpointAntiColision,
		client:               c.client,
		apiClient:            c.apiClient,
		tokens:               c.tokens,
		hub:                  c.hub,
		m3uCache:             map[string]cachedM3U{},
		rules:                c.rules,
		tunerRules:           c.tunerRules,
		ffmpeg:               c.ffmpeg,
		ffmpegWait:           c.ffmpegWait,
		lastGood:             newLastGood(),
		users:                users,
		usersByName:          map[string]*proxyUser{},
		sources:              sources,
		m3uInputs:            c.m3uInputs,
		view:                 view,
	}
	d.merge.categories = map[string]shownCategories{}
	for _, u := range users {
		d.usersByName[u.name.String()] = u
	}
	return d
}

// setupTuners builds the HDHomeRun tuners of the settings: each serves the
// combined catalogue, or one source alone.
func (c *Config) setupTuners() error {
	ports := map[int]bool{c.HostConfig.Port: true}
	if c.HDHomeRunPort != 0 {
		ports[c.HDHomeRunPort] = true
	}
	for _, t := range c.Tuners {
		if ports[t.Port] {
			return fmt.Errorf("tuner: port %d is used twice, or is the proxy's", t.Port)
		}
		ports[t.Port] = true

		own, err := filter.New(t.Filter)
		if err != nil {
			return fmt.Errorf("the HDHomeRun tuner on port %d: %w", t.Port, err)
		}
		var tc *Config
		if t.Source == "" {
			tc = c.derive(*c.ProxyConfig, c.sources, nil, false)
		} else {
			i := slices.IndexFunc(c.sources, func(s *source) bool { return s.name == t.Source })
			if i < 0 {
				return fmt.Errorf("the HDHomeRun tuner on port %d: no source is named %q", t.Port, t.Source)
			}
			tc = c.newView(c.sources[i])
			tc.users, tc.usersByName = nil, map[string]*proxyUser{}
		}
		tc.HDHomeRunPort, tc.HDHomeRunTuners, tc.HDHomeRunFilter = t.Port, t.Count, t.Filter
		tc.tunerRules = filter.Combine(c.rules, own)
		tc.tunerClone = true
		c.tuners = append(c.tuners, tc)
	}
	return nil
}
