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

package cmd

import (
	"errors"
	"fmt"
	"slices"

	"github.com/spf13/viper"

	"github.com/pierre-emmanuelJ/iptv-proxy/pkg/config"
	"github.com/pierre-emmanuelJ/iptv-proxy/pkg/filter"
)

const (
	inputXtream = config.SourceXtream
	inputM3U    = config.SourceM3U

	dedupeGuideID = "guide-id"
)

// fileInput is an input as the configuration file writes it. What the
// proxy does not do yet is read all the same, to be refused.
type fileInput struct {
	Name           string `mapstructure:"name"`
	Type           string `mapstructure:"type"`
	BaseURL        string `mapstructure:"base-url"`
	User           string `mapstructure:"user"`
	Password       string `mapstructure:"password"`
	MaxConnections int    `mapstructure:"max-connections"`
	URL            string `mapstructure:"url"`
	XMLTVURL       string `mapstructure:"xmltv-url"`

	Path           string `mapstructure:"path"`
	Individual     bool   `mapstructure:"individual"`
	BaseID         *int   `mapstructure:"base-id"`
	CategoryPrefix string `mapstructure:"category-prefix"`
}

// fileOutput is the output section of the configuration file.
type fileOutput struct {
	Formats           []string    `mapstructure:"formats"`
	Combined          *bool       `mapstructure:"combined"`
	Dedupe            string      `mapstructure:"dedupe"`
	CategorySeparator string      `mapstructure:"category-separator"`
	HDHomeRun         []fileTuner `mapstructure:"hdhomerun"`
}

// fileTuner is an HDHomeRun tuner of the output section.
type fileTuner struct {
	Port                int      `mapstructure:"port"`
	Tuners              int      `mapstructure:"tuners"`
	FFmpeg              string   `mapstructure:"ffmpeg"`
	Sources             []string `mapstructure:"sources"`
	GroupRegex          string   `mapstructure:"group-regex"`
	ChannelRegex        string   `mapstructure:"channel-regex"`
	GroupExcludeRegex   string   `mapstructure:"group-exclude-regex"`
	ChannelExcludeRegex string   `mapstructure:"channel-exclude-regex"`
}

// legacyInputOptions are the options that name an input without the
// configuration file's inputs.
var legacyInputOptions = []string{"m3u-url", "xtream-base-url", "xtream-user", "xtream-password"}

// legacyTunerOptions are the options that set up the tuner without the
// configuration file's output.
var legacyTunerOptions = []string{
	"hdhomerun-port", "hdhomerun-tuners",
	"hdhomerun-group-regex", "hdhomerun-channel-regex",
	"hdhomerun-group-exclude-regex", "hdhomerun-channel-exclude-regex",
}

// applyInputsAndOutput reads the inputs and the output of the configuration
// file into conf. Without them, conf is left to the options.
func applyInputsAndOutput(conf *config.ProxyConfig) error {
	var out *fileOutput
	if viper.IsSet("output") {
		out = &fileOutput{}
		if err := viper.UnmarshalKey("output", out); err != nil {
			return fmt.Errorf("output in the configuration file: %w", err)
		}
		if err := applyOutput(conf, out); err != nil {
			return fmt.Errorf("output: %w", err)
		}
	}
	if viper.IsSet("inputs") {
		var listed []fileInput
		if err := viper.UnmarshalKey("inputs", &listed); err != nil {
			return fmt.Errorf("inputs in the configuration file: %w", err)
		}
		if err := applyInputs(conf, listed, out); err != nil {
			return fmt.Errorf("inputs: %w", err)
		}
	}
	if conf.NoCombined && !conf.XtreamIndividual && !slices.ContainsFunc(conf.Sources, func(s config.Source) bool { return s.Individual }) {
		return errors.New("output: combined is false and no input is individual: nothing would be served")
	}
	return nil
}

func applyOutput(conf *config.ProxyConfig, out *fileOutput) error {
	for _, option := range legacyTunerOptions {
		if viper.GetString(option) != "" && viper.GetString(option) != "0" {
			return fmt.Errorf("--%s goes with no output section: set the tuner in output.hdhomerun", option)
		}
	}

	formats := []string{}
	for _, f := range out.Formats {
		switch f {
		case config.OutputXtream, config.OutputM3U, config.OutputXMLTV:
			if !slices.Contains(formats, f) {
				formats = append(formats, f)
			}
		default:
			return fmt.Errorf("unknown format %q: formats are %s, %s and %s", f, config.OutputXtream, config.OutputM3U, config.OutputXMLTV)
		}
	}
	conf.Output = &config.Output{Formats: formats, CategorySeparator: out.CategorySeparator}

	switch {
	case out.Combined != nil && !*out.Combined:
		conf.NoCombined = true
	case out.Dedupe != "" && out.Dedupe != dedupeGuideID:
		return fmt.Errorf("dedupe: %q is not supported yet: only %s is", out.Dedupe, dedupeGuideID)
	}

	switch len(out.HDHomeRun) {
	case 0:
	case 1:
		t := out.HDHomeRun[0]
		switch {
		case t.Port <= 0:
			return errors.New("hdhomerun needs a port")
		case len(t.Sources) > 0:
			return errors.New("hdhomerun: sources is not supported yet")
		}
		conf.HDHomeRunPort = t.Port
		conf.HDHomeRunTuners = t.Tuners
		if t.FFmpeg != "" {
			conf.FFmpeg = t.FFmpeg
		}
		conf.HDHomeRunFilter = filter.Patterns{Group: t.GroupRegex, Channel: t.ChannelRegex, GroupExclude: t.GroupExcludeRegex, ChannelExclude: t.ChannelExcludeRegex}
	default:
		return errors.New("hdhomerun: one tuner only, for now")
	}
	return nil
}

func applyInputs(conf *config.ProxyConfig, listed []fileInput, out *fileOutput) error {
	for _, option := range legacyInputOptions {
		if viper.GetString(option) != "" {
			return fmt.Errorf("--%s goes with no inputs: list the provider among them", option)
		}
	}
	if viper.GetBool("xtream-passthrough") {
		return errors.New("--xtream-passthrough goes with no inputs: it is not supported among them yet")
	}
	if viper.IsSet("sources") {
		return errors.New("sources goes with no inputs: list the accounts among them")
	}
	if len(listed) == 0 {
		return errors.New("none listed")
	}
	if len(listed) > 1 && (out == nil || out.Dedupe != dedupeGuideID) {
		return fmt.Errorf("several inputs are combined as one catalogue, where a channel with the same guide id is shown once: set output.dedupe to %s", dedupeGuideID)
	}

	names := map[string]bool{}
	var sources []config.Source
	for i, in := range listed {
		name := in.Name
		if name == "" {
			name = fmt.Sprintf("input %d", i+1)
		}
		if names[name] {
			return fmt.Errorf("%s: the name is used twice", name)
		}
		names[name] = true

		switch {
		case in.Type == "":
			return fmt.Errorf("%s: type is missing", name)
		case in.Type != inputXtream && in.Type != inputM3U:
			return fmt.Errorf("%s: type %q is not supported yet: only %s and %s are", name, in.Type, inputXtream, inputM3U)
		case in.Type == inputXtream && (in.BaseURL == "" || in.User == "" || in.Password == ""):
			return fmt.Errorf("%s: an xtream input needs base-url, user and password", name)
		case in.Type == inputXtream && (in.URL != "" || in.XMLTVURL != ""):
			return fmt.Errorf("%s: url and xmltv-url are for an m3u input", name)
		case in.Type == inputM3U && in.URL == "":
			return fmt.Errorf("%s: an m3u input needs its url", name)
		case in.Type == inputM3U && (in.BaseURL != "" || in.User != "" || in.Password != ""):
			return fmt.Errorf("%s: base-url, user and password are for an xtream input", name)
		case in.Individual && in.Path == "":
			return fmt.Errorf("%s: individual needs a path", name)
		case in.Path != "" && !in.Individual:
			return fmt.Errorf("%s: path is for an input that is individual", name)
		case len(listed) == 1 && in.Individual:
			return fmt.Errorf("%s: individual only counts among several inputs", name)
		case len(listed) == 1 && in.CategoryPrefix != "":
			return fmt.Errorf("%s: category-prefix only counts among several inputs", name)
		case len(listed) == 1 && in.MaxConnections != 0:
			return fmt.Errorf("%s: max-connections only counts among several inputs", name)
		case len(listed) == 1 && in.BaseID != nil:
			return fmt.Errorf("%s: base-id only counts among several inputs", name)
		case in.BaseID != nil && *in.BaseID < 0:
			return fmt.Errorf("%s: base-id cannot be negative", name)
		}

		// The first input is the proxy's Xtream account, when it is one.
		if i == 0 && in.Type == inputXtream {
			conf.XtreamName = name
			conf.XtreamBaseURL = in.BaseURL
			conf.XtreamUser = config.CredentialString(in.User)
			conf.XtreamPassword = config.CredentialString(in.Password)
			conf.XtreamMaxConnections = in.MaxConnections
			conf.XtreamBaseID = in.BaseID
			conf.XtreamCategoryPrefix = in.CategoryPrefix
			conf.XtreamPath, conf.XtreamIndividual = in.Path, in.Individual
			continue
		}
		sources = append(sources, config.Source{
			Name: name, Type: in.Type,
			XtreamBaseURL: in.BaseURL, XtreamUser: in.User, XtreamPassword: in.Password,
			URL: in.URL, XMLTVURL: in.XMLTVURL,
			MaxConnections: in.MaxConnections, BaseID: in.BaseID, CategoryPrefix: in.CategoryPrefix,
			Path: in.Path, Individual: in.Individual,
		})
	}
	conf.Sources = sources
	return nil
}
