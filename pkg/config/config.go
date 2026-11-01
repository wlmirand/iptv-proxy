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

package config

import (
	"net/url"

	"github.com/pierre-emmanuelJ/iptv-proxy/pkg/filter"
)

// CredentialString represents an iptv-proxy credential.
type CredentialString string

// PathEscape escapes the credential for an url path.
func (c CredentialString) PathEscape() string {
	return url.PathEscape(string(c))
}

// String returns the credential string.
func (c CredentialString) String() string {
	return string(c)
}

// HostConfiguration containt host infos
type HostConfiguration struct {
	Hostname string
	Port     int
}

// User is one of the users of the configuration file.
type User struct {
	Name, Password string
	// MaxConnections is how many streams the user may watch at once; 0 is
	// no limit.
	MaxConnections int
	// Filter keeps the channels the user sees, after the proxy's filters.
	Filter filter.Patterns
}

// Source is an Xtream provider account the proxy serves besides the one of
// the Xtream options, or an M3U playlist (Type SourceM3U).
type Source struct {
	// Name names the source in logs.
	Name string
	// Type is SourceXtream, or SourceM3U; empty is Xtream.
	Type                                      string
	XtreamBaseURL, XtreamUser, XtreamPassword string
	// URL is the playlist of an M3U source: an http(s) address or a local
	// file. XMLTVURL is its guide, when the playlist does not name one.
	URL, XMLTVURL string
	// Path is where the source's own view is served ("/<path>/..."), when
	// Individual says so: its channels alone, as they are.
	Path       string
	Individual bool
	// CategoryPrefix is put in front of the source's categories in the
	// combined catalogue, with the Output's separator.
	CategoryPrefix string
	// MaxConnections is how many streams the proxy opens at once on the
	// account; 0 asks the provider.
	MaxConnections int
	// BaseID is where the source's range of ids starts, in the catalogue
	// the sources make together; nil follows the source's position.
	BaseID *int
}

// The types of a source.
const (
	SourceXtream = "xtream"
	SourceM3U    = "m3u"
)

// What the proxy delivers to its clients.
const (
	// OutputXtream is the Xtream Codes API (player_api.php, get.php) and the
	// streams it names.
	OutputXtream = "xtream"
	// OutputM3U is the M3U playlist (M3UFileName) and the streams it names.
	OutputM3U = "m3u"
	// OutputXMLTV is the guide at /xmltv.php.
	OutputXMLTV = "xmltv"
)

// Output is what the proxy delivers, when the configuration file says so.
// The HDHomeRun tuner is chosen by its own options.
type Output struct {
	Formats []string
	// CategorySeparator goes between a source's category prefix and the
	// category; empty is DefaultCategorySeparator.
	CategorySeparator string
}

// DefaultCategorySeparator is the separator between a category prefix and a
// category.
const DefaultCategorySeparator = " | "

// Separator is the string between a category prefix and a category.
func (o *Output) Separator() string {
	if o == nil || o.CategorySeparator == "" {
		return DefaultCategorySeparator
	}
	return o.CategorySeparator
}

// Has tells whether the proxy delivers a format. Without an Output, it
// delivers everything its inputs allow.
func (o *Output) Has(format string) bool {
	if o == nil {
		return true
	}
	for _, f := range o.Formats {
		if f == format {
			return true
		}
	}
	return false
}

// ProxyConfig Contain original m3u playlist and HostConfiguration
type ProxyConfig struct {
	HostConfig     *HostConfiguration
	XtreamUser     CredentialString
	XtreamPassword CredentialString
	XtreamBaseURL  string
	// XtreamName names the Xtream account in logs, and XtreamMaxConnections
	// is its limit of streams at once (0 asks the provider) when it is one
	// of several sources.
	XtreamName           string
	XtreamMaxConnections int
	XtreamBaseID         *int
	XtreamCategoryPrefix string
	XtreamPath           string
	XtreamIndividual     bool
	// NoCombined leaves out the combined catalogue: only the sources'
	// own views are served.
	NoCombined           bool
	XtreamGenerateApiGet bool
	// XtreamApiGetMovies adds the provider's movies to the playlist generated
	// from its API. Off by default: a catalogue of tens of thousands of
	// movies makes a playlist some players cannot load.
	XtreamApiGetMovies bool
	M3UCacheExpiration int
	M3UFileName        string
	CustomEndpoint     string
	CustomId           string
	RemoteURL          *url.URL
	AdvertisedPort     int
	HTTPS              bool
	User, Password     CredentialString
	// UserAgent, when set, replaces the client's on every request to the
	// provider.
	UserAgent string
	// NoStreamSharing gives every client its own connection to the provider
	// for a live stream, instead of one connection per stream.
	NoStreamSharing bool
	// Filter keeps the channels clients see, by group and by name.
	Filter filter.Patterns
	// ListenAddress is the address the proxy listens on; empty means every
	// interface.
	ListenAddress string
	// XMLTVURL is the guide of an M3U playlist, when its header does not
	// name one or names another.
	XMLTVURL string
	// ProxyLogos serves the logos and covers of playlists and of the Xtream
	// API through the proxy, like the streams.
	ProxyLogos bool
	// HDHomeRunPort, when set, is the port of an HDHomeRun tuner serving the
	// live channels to media servers (Plex...). HDHomeRunTuners is the
	// number of tuners it announces; 0 asks the provider.
	HDHomeRunPort   int
	HDHomeRunTuners int
	// HDHomeRunFilter keeps the tuner's channels, after the proxy's filters.
	HDHomeRunFilter filter.Patterns
	// FFmpeg is the ffmpeg the tuner turns HLS channels into MPEG-TS with:
	// a command found in the PATH or a path; "none" goes without.
	FFmpeg string
	// Users, when given, are the users of the proxy instead of the one of
	// User and Password.
	Users []User
	// MaxConnections is the limit of streams at once of the one user of User
	// and Password; 0 is no limit.
	MaxConnections int
	// XtreamPassthrough lets each client log in with its own account of the
	// Xtream provider, instead of the proxy's users and account.
	XtreamPassthrough bool
	// Sources are more Xtream accounts, served with the one of the Xtream
	// options as one catalogue.
	Sources []Source
	// Output, when set, is what the proxy delivers; nil delivers everything.
	Output *Output
	// StatusPassword, when set, opens a read-only status page at /status.
	StatusPassword CredentialString
	// Version is the proxy's version, shown on the status page.
	Version string
}
