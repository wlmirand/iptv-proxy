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

	"github.com/pierre-emmanuelJ/iptv-proxy/pkg/config"
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
	conf.Sources = nil
	conf.NoCombined = false
	// one status page, and one tuner: the combined catalogue's
	conf.StatusPassword = ""
	conf.HDHomeRunPort = 0

	// the source as it is, on its own: no range of ids, no prefix
	own := *src
	own.index, own.base, own.baseID, own.prefix = 0, 0, nil, ""

	v := &Config{
		ProxyConfig:          &conf,
		endpointAntiColision: c.endpointAntiColision,
		client:               c.client,
		apiClient:            c.apiClient,
		tokens:               c.tokens,
		hub:                  c.hub,
		m3uCache:             map[string]cachedM3U{},
		rules:                c.rules,
		tunerRules:           c.tunerRules,
		lastGood:             newLastGood(),
		usersByName:          map[string]*proxyUser{},
		sources:              []*source{&own},
		m3uInputs:            c.m3uInputs,
		view:                 true,
	}
	v.merge.categories = map[string]shownCategories{}
	for _, u := range c.users {
		own := *u
		own.provider = src.account
		v.users = append(v.users, &own)
		v.usersByName[u.name.String()] = &own
	}
	return v
}
