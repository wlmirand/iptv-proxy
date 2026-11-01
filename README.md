# iptv-proxy

[![CI](https://github.com/pierre-emmanuelJ/iptv-proxy/actions/workflows/ci.yml/badge.svg?branch=master)](https://github.com/pierre-emmanuelJ/iptv-proxy/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/pierre-emmanuelJ/iptv-proxy)](https://github.com/pierre-emmanuelJ/iptv-proxy/releases/latest)
[![Docker pulls](https://img.shields.io/docker/pulls/pierro777/iptv-proxy)](https://hub.docker.com/r/pierro777/iptv-proxy)

iptv-proxy is a reverse proxy for IPTV: give it an M3U/M3U8 playlist or an
Xtream Codes account, and it serves the same channels, movies and series from
your own address, with a user and a password you choose.

Your players (TiviMate, IPTV Smarters, VLC, Kodi...) talk to the proxy and
never see the provider's address or credentials. Media servers (Plex,
Jellyfin, Emby, Channels DVR) can see the live channels as an HDHomeRun TV
tuner, with their guide. Use it to share IPTV at home without handing out the
provider's login, to put a provider behind your own domain, HTTPS or VPN, or
to give every device one address that does not change.

It is one binary or one container, set up with a few flags or environment
variables. There is no database and nothing to configure in a web interface;
a read-only status page shows what is being watched.

## Quick start

In the examples, `192.168.1.10` is the machine running the proxy, as your
players reach it, and `family` / `choose-a-password` are the credentials you
give to your players. Replace them with your own.

### Docker, with an M3U playlist

```sh
docker run -d --name iptv-proxy -p 8080:8080 \
  -e M3U_URL="http://provider.example:8080/playlist.m3u" \
  -e PROXY_HOSTNAME=192.168.1.10 \
  -e PROXY_USER=family \
  -e PROXY_PASSWORD=choose-a-password \
  -e GIN_MODE=release \
  pierro777/iptv-proxy:latest
```

Give this address to your player:

```
http://192.168.1.10:8080/iptv.m3u?username=family&password=choose-a-password
```

To check it from a terminal:

```sh
curl "http://192.168.1.10:8080/iptv.m3u?username=family&password=choose-a-password"
```

The playlist can also be a local file: mount it and give its path.

```sh
docker run -d --name iptv-proxy -p 8080:8080 \
  -v "$PWD/iptv.m3u:/iptv.m3u:ro" \
  -e M3U_URL=/iptv.m3u \
  -e PROXY_HOSTNAME=192.168.1.10 \
  -e PROXY_USER=family \
  -e PROXY_PASSWORD=choose-a-password \
  -e GIN_MODE=release \
  pierro777/iptv-proxy:latest
```

### Docker, with an Xtream Codes account

```sh
docker run -d --name iptv-proxy -p 8080:8080 \
  -e XTREAM_BASE_URL="http://provider.example:8080" \
  -e XTREAM_USER=xtream_user \
  -e XTREAM_PASSWORD=xtream_password \
  -e PROXY_HOSTNAME=192.168.1.10 \
  -e PROXY_USER=family \
  -e PROXY_PASSWORD=choose-a-password \
  -e GIN_MODE=release \
  pierro777/iptv-proxy:latest
```

In your player, choose the Xtream Codes login and enter:

```
Server:   http://192.168.1.10:8080
Username: family
Password: choose-a-password
```

A player that only takes a playlist can use the usual Xtream addresses:

```
Playlist: http://192.168.1.10:8080/get.php?username=family&password=choose-a-password&type=m3u_plus&output=ts
Guide:    http://192.168.1.10:8080/xmltv.php?username=family&password=choose-a-password
```

### Docker images

| Registry | Image |
|---|---|
| Docker Hub | `pierro777/iptv-proxy` |
| GitHub | `ghcr.io/pierre-emmanuelj/iptv-proxy` |

Tags: `latest`, and for a given version `v4.7.1`, `v4.7` or `v4` (`v3` for the
3.x versions). Since v3.12.0 they work on amd64 and on ARM (Raspberry Pi, Apple silicon): Docker
pulls the image of your machine. The `-amd64` and `-arm64` tags
(`pierro777/iptv-proxy:latest-arm64`) name one architecture, as before.

Each tag also has an image with ffmpeg, about 140 MB larger: `latest-ffmpeg`,
`v4-ffmpeg`, `v4.7.0-ffmpeg`... Take it if the HDHomeRun tuner serves
channels your provider has only as HLS (see
[Plex and other media servers](#plex-and-other-media-servers)); nothing
else needs it.

### Docker Compose

```yaml
services:
  iptv-proxy:
    image: pierro777/iptv-proxy:latest
    restart: unless-stopped
    ports:
      # the same port as PORT below
      - 8080:8080
    environment:
      # Either an M3U playlist (an address, or a file mounted in the container)...
      M3U_URL: "http://provider.example:8080/playlist.m3u"
      # ...or an Xtream Codes account (remove M3U_URL then)
      # XTREAM_BASE_URL: "http://provider.example:8080"
      # XTREAM_USER: xtream_user
      # XTREAM_PASSWORD: xtream_password
      PORT: 8080
      # Name or IP your players reach this machine at
      PROXY_HOSTNAME: 192.168.1.10
      # What your players log in with
      PROXY_USER: family
      PROXY_PASSWORD: choose-a-password
      GIN_MODE: release
```

```sh
docker compose up -d
```

The [`docker-compose.yml`](docker-compose.yml) of this repository does the
same with an Xtream Codes account, building the image from the sources.

### Binary

Download the archive for your system (Linux, macOS, Windows; amd64 and arm64;
also `.deb` and `.rpm`) from the
[releases](https://github.com/pierre-emmanuelJ/iptv-proxy/releases/latest),
or build it with Go 1.27 or later:

```sh
git clone https://github.com/pierre-emmanuelJ/iptv-proxy.git
cd iptv-proxy
go build -o iptv-proxy .
```

```sh
GIN_MODE=release ./iptv-proxy \
  --m3u-url "http://provider.example:8080/playlist.m3u" \
  --hostname 192.168.1.10 \
  --user family \
  --password choose-a-password
```

Quote the playlist address: it usually contains `?` and `&`.

## Options

Every option is a flag, an environment variable (the flag's name in upper
case, with `_` instead of `-`) or a line of the configuration file. A flag
wins over the variable, and the variable over the file.

| Flag | Environment variable | Default | What it does |
|---|---|---|---|
| `--m3u-url`, `-u` | `M3U_URL` | | Provider playlist: an `http(s)` address or a local file. |
| `--xtream-base-url` | `XTREAM_BASE_URL` | | Provider's Xtream Codes address, e.g. `http://provider.example:8080`. |
| `--xtream-user` | `XTREAM_USER` | | Provider's Xtream Codes user. |
| `--xtream-password` | `XTREAM_PASSWORD` | | Provider's Xtream Codes password. |
| `--xtream-passthrough` | `XTREAM_PASSTHROUGH` | `false` | Players log in with their own account of the provider. See [Each player with its own provider account](#each-player-with-its-own-provider-account). |
| `--hostname` | `PROXY_HOSTNAME` (or `HOSTNAME`) | | Name or IP your players reach the proxy at. It is written in every address the proxy gives out. |
| `--port` | `PORT` | `8080` | Port the proxy listens on. |
| `--listen-address` | `LISTEN_ADDRESS` | every interface | IP address the proxy listens on, e.g. `127.0.0.1` behind a reverse proxy on the same machine. |
| `--advertised-port` | `ADVERTISED_PORT` | value of `--port` | Port written in the addresses the proxy gives out, when it differs from the listening port (behind a reverse proxy, or a different published Docker port). |
| `--https` | `HTTPS` | `false` | Write `https://` instead of `http://` in the addresses the proxy gives out. The proxy itself always listens in plain HTTP: put a reverse proxy in front for TLS. |
| `--user` | `PROXY_USER` (or `USER`) | `usertest` | User your players log in with. |
| `--password` | `PROXY_PASSWORD` (or `PASSWORD`) | `passwordtest` | Password your players log in with. |
| `--max-connections` | `MAX_CONNECTIONS` | `0` (no limit) | Streams that user may watch at once. See [Several users](#several-users). |
| `--m3u-file-name` | `M3U_FILE_NAME` | `iptv.m3u` | Name of the playlist the proxy serves: `http://host:port/iptv.m3u`. |
| `--custom-endpoint` | `CUSTOM_ENDPOINT` | | Prefix put before every path: `http://host:port/<custom-endpoint>/iptv.m3u`. |
| `--custom-id` | `CUSTOM_ID` | derived from your settings | First path element of M3U track addresses. The same settings give the same addresses, restart after restart. |
| `--m3u-cache-expiration` | `M3U_CACHE_EXPIRATION` | `1` | Hours a playlist is kept before reading it again from the provider (M3U playlists included since v3.13.0). |
| `--xmltv-url` | `XMLTV_URL` | the guide the playlist names | Guide (XMLTV) of an M3U playlist: an `http(s)` address or a local file. It is served at `/xmltv.php`. |
| `--xtream-api-get` | `XTREAM_API_GET` | `false` | Build the `get.php` playlist (live channels) from the provider's API, for providers that disabled `get.php`. |
| `--xtream-api-get-movies` | `XTREAM_API_GET_MOVIES` | `false` | Add the provider's movies to that generated playlist. Off by default: a catalogue of tens of thousands of movies makes a playlist some players cannot load. Series are not added (see below). |
| `--user-agent` | `USER_AGENT` | | User-Agent sent to the provider instead of the player's. Some providers only answer known players. |
| `--no-stream-sharing` | `NO_STREAM_SHARING` | `false` | Open one provider connection per player for a live stream, instead of sharing one between the players watching it. |
| `--status-password` | `STATUS_PASSWORD` | | Password of a read-only status page at `/status`. See [Status page](#status-page). |
| `--proxy-logos` | `PROXY_LOGOS` | `false` | Serve the logos and covers of playlists, of the Xtream API and of the guides through the proxy too, so that players reach no other host. |
| `--hdhomerun-port` | `HDHOMERUN_PORT` | | Port of an HDHomeRun tuner for Plex, Emby, Jellyfin or Channels DVR. Local network only. See [Plex and other media servers](#plex-and-other-media-servers). |
| `--hdhomerun-group-regex`, `--hdhomerun-channel-regex`, `--hdhomerun-group-exclude-regex`, `--hdhomerun-channel-exclude-regex` | `HDHOMERUN_GROUP_REGEX`... | | Filters of the tuner alone, applied after the proxy's: the media server gets fewer channels than your players. |
| `--hdhomerun-tuners` | `HDHOMERUN_TUNERS` | the account's connections (the sources' together), else `2` | Number of tuners announced: how many channels a media server plays at once. |
| `--ffmpeg` | `FFMPEG` | `ffmpeg` | ffmpeg the HDHomeRun tuner turns HLS channels into MPEG-TS with, when it is found (the `-ffmpeg` images have it). `none` goes without. |
| `--group-regex` | `GROUP_REGEX` | | Keep only the live channels whose group matches this regular expression. See [Filtering channels](#filtering-channels). |
| `--channel-regex` | `CHANNEL_REGEX` | | Keep only the live channels whose name matches this regular expression. |
| `--group-exclude-regex` | `GROUP_EXCLUDE_REGEX` | | Leave out the live channels whose group matches this regular expression. |
| `--channel-exclude-regex` | `CHANNEL_EXCLUDE_REGEX` | | Leave out the live channels whose name matches this regular expression. |
| `--iptv-proxy-config` | `IPTV_PROXY_CONFIG` | `.iptv-proxy.yaml` in the home or current directory | YAML file holding the same options, named as the flags (`m3u-url: ...`), the [users](#several-users), the [sources](#several-sources), and the [inputs and output](#several-inputs-one-catalogue-or-one-each). |
| `--version` | | | Prints the version and exits. |

Good to know:

- Change `--user` and `--password`: the defaults are public.
- `USER` and `HOSTNAME` are also ordinary system variables: a shell sets
  `USER` to your login name, and Docker sets `HOSTNAME` to the container's ID.
  Since v3.11.0, prefer `PROXY_USER`, `PROXY_PASSWORD` and `PROXY_HOSTNAME`:
  they win over the old names, which keep working. Since v3.12.0, `user`,
  `password` and `hostname` in the configuration file also win over the old
  names.
- `GIN_MODE` is not an option of the proxy but of its web framework. Since
  v3.10.0 the proxy runs in `release` mode unless you set it. Before that,
  set `GIN_MODE=release` as the examples do: the debug mode lists the routes
  at startup, and they contain your user and password. The access log masks
  them in both modes.

## M3U playlists

The proxy reads the provider's playlist at startup and serves it at
`/iptv.m3u` with every track address replaced by its own. All other lines
(names, logos, groups, guide IDs, player options) are kept as the provider
wrote them. When a player asks for it and it is older than
`--m3u-cache-expiration` (one hour by default), the playlist is read again: no
restart is needed to pick up the provider's changes. If the provider fails,
the previous playlist is kept.

A track's address on the proxy is derived from its address at the provider:
it stays the same when the playlist is read again, wherever the track moves
in it, and from one start of the proxy to the next.

The provider's playlist:

```m3u
#EXTM3U
#EXTINF:-1 tvg-id="news.example" tvg-name="News" tvg-logo="http://provider.example/logos/news.png" group-title="News",News HD
http://provider.example:8080/live/news/1.ts
#EXTINF:-1 tvg-id="sport.example" tvg-name="Sport" tvg-logo="http://provider.example/logos/sport.png" group-title="Sport",Sport HD
http://provider.example:8080/live/sport/2.m3u8?token=abc
```

What your players get:

```m3u
#EXTM3U
#EXTINF:-1 tvg-id="news.example" tvg-name="News" tvg-logo="http://provider.example/logos/news.png" group-title="News",News HD
http://192.168.1.10:8080/e3c0c308/family/choose-a-password/4f2a9c1d7b21e0aa/1.ts
#EXTINF:-1 tvg-id="sport.example" tvg-name="Sport" tvg-logo="http://provider.example/logos/sport.png" group-title="Sport",Sport HD
http://192.168.1.10:8080/e3c0c308/family/choose-a-password/9b03e5c2d8a17f46/2.m3u8
```

`e3c0c308` is the `--custom-id`, and `4f2a9c1d7b21e0aa` names the track.
Tokens and other query parameters of the provider's addresses stay on the
proxy.

When the playlist names a guide (`url-tvg` or `x-tvg-url` in its first line),
or `--xmltv-url` gives one, the proxy serves it at
`/xmltv.php?username=...&password=...`, and the playlist names that address
instead. Players load the guide through the proxy, and the filters apply to
it.

Some playlists name the provider's credentials outside of the track
addresses too: a guide address in `url-tvg`, a catch-up address in
`catchup-source`, a player option. Such an attribute or line is removed, so
that the credentials never reach a player. (In an Xtream Codes playlist, they
point to the proxy instead: see below.)

## Xtream Codes

The proxy answers the Xtream Codes client API like the provider does: live,
movies, series, catch-up and the guide. Your players log in with the proxy's
address and credentials, the proxy asks the provider with the real ones.

| | Provider | Proxy |
|---|---|---|
| Server | `http://provider.example:8080` | `http://192.168.1.10:8080` |
| User | `xtream_user` | `family` |
| Password | `xtream_password` | `choose-a-password` |

The provider's answers are passed on as they are, so the fields and quirks of
any provider reach the player. Only what names the provider is rewritten: the
account and server of the login answer, and the addresses holding its
credentials. In the `get.php` playlist, the guide (`url-tvg`) and catch-up
(`catchup-source`) addresses of the provider become the proxy's, and work
through it.

Endpoints served:

| Endpoint | What it is |
|---|---|
| `/player_api.php` | The client API (login, categories, streams, movie and series details, short guide). |
| `/get.php` | The M3U playlist, with the same parameters as the provider's (`type`, `output`). |
| `/xmltv.php` | The full guide (XMLTV), streamed from the provider. |
| `/apiget` | A playlist of the live channels built from the API. |
| `/<user>/<password>/<id>`, `/live/...`, `/movie/...`, `/series/...`, `/timeshift/...` | The streams. |

`/player_api.php`, `/get.php`, `/xmltv.php` and `/apiget` take
`username=...&password=...`.

If you give `--m3u-url` the provider's `get.php` address
(`http://provider.example:8080/get.php?username=xtream_user&password=xtream_password&type=m3u_plus&output=ts`),
the Xtream options are read from it: everything above is served, and that
playlist is also available at `/iptv.m3u`.

With the Xtream options alone, `/iptv.m3u` is the account's playlist too (the
same as `/get.php`).

The playlist built from the API (`/apiget`, or `/get.php` with
`--xtream-api-get`) holds the live channels, and the movies with
`--xtream-api-get-movies`. Series are not in it: the API gives the episodes of
one series at a time, so listing them all would take one request per series.
Players that speak the Xtream API get movies and series from it directly.

## HLS

HLS streams (`.m3u8`) work in both modes, whatever the provider. Every address
an HLS playlist names (variants, segments, keys, audio tracks) is replaced by
an address of the proxy, `/hls/<token>/<name>`, so the whole stream goes
through the proxy, on any host and after any redirect. The token is the
provider's address, encrypted: a player never sees the provider's host,
credentials or session tokens. Nothing is stored, and tokens stay valid across
restarts.

## Several users

One user and password is all most homes need, and the options above give
exactly that. To give each person or device their own login, list the users
in the configuration file (`--iptv-proxy-config`, or `IPTV_PROXY_CONFIG` in
Docker):

```yaml
# /config/iptv-proxy.yaml
users:
  - name: family
    password: choose-a-password
    max-connections: 2
  - name: kids
    password: another-password
    max-connections: 1
    group-regex: "(?i)kids|cartoon"
  - name: grandma
    password: a-third-one
    group-regex: "^FR "
    channel-exclude-regex: "(?i)adult"
```

```sh
docker run -d --name iptv-proxy -p 8080:8080 \
  -v "$PWD/iptv-proxy.yaml:/config/iptv-proxy.yaml:ro" \
  -e IPTV_PROXY_CONFIG=/config/iptv-proxy.yaml \
  -e XTREAM_BASE_URL="http://provider.example:8080" \
  -e XTREAM_USER=xtream_user \
  -e XTREAM_PASSWORD=xtream_password \
  -e PROXY_HOSTNAME=192.168.1.10 \
  pierro777/iptv-proxy:latest
```

- When `users` is there, those users replace `--user` and `--password`.
  The other options (provider, address, filters) stay where they are: flags,
  variables or the same file.
- **Each user logs in with their own name and password**, and their playlists
  and stream addresses hold their own credentials.
- **`max-connections`** is how many streams the user watches at once (none
  means no limit). At the limit, a new stream from the same device (the same
  address) replaces that device's oldest one: zapping keeps working. A stream
  from another device is refused until one stops. The login answer gives
  players the user's limit and the streams they watch. HLS segments are not
  counted: a live HLS channel is many short requests.
- **Filters** (`group-regex`, `channel-regex`, `group-exclude-regex`,
  `channel-exclude-regex`) apply to that user on top of the proxy's own, with
  the same rules (see [Filtering channels](#filtering-channels)): a live
  channel left out does not play, even asked by its id. Movies and series are
  not filtered.
- Names must not be `hls`, `logo`, `live`, `movie`, `series`, `timeshift` or
  `play`: they are the first elements of the proxy's own addresses.
- `--max-connections` (`MAX_CONNECTIONS`) sets the limit of the one user of
  `--user` and `--password`, without a file.

## Each player with its own provider account

When everyone in the house has their own account with the same provider,
the proxy does not need one: with `--xtream-passthrough`, players log in to
the proxy with their provider account, and the proxy only gives the
provider's address.

```sh
docker run -d --name iptv-proxy -p 8080:8080 \
  -e XTREAM_BASE_URL="http://provider.example:8080" \
  -e XTREAM_PASSTHROUGH=true \
  -e PROXY_HOSTNAME=192.168.1.10 \
  pierro777/iptv-proxy:latest
```

- Players are set up with the proxy's address and **their own provider user
  and password**. Their playlists, guide and stream addresses name the proxy
  and hold their own account; the provider's address is left out.
- The proxy asks the provider whether it accepts an account at the first
  request, then again every 10 minutes. An account refused by the provider
  gets a 401. An account it accepted keeps playing while the provider cannot
  answer, with the [last good answers](#when-the-provider-fails).
- **Each account is held to the streams the provider allows it**
  (`max_connections` of its login answer), with the same rule as
  [several users](#several-users): zapping on the same device works, another
  device waits. `--max-connections` sets another limit for every account.
- The proxy's filters apply to every account. Live streams are shared
  between the players of the same account only.
- There are no proxy users: `--xtream-user`, `--xtream-password`, `users` in
  the configuration file, `--m3u-url` and the HDHomeRun tuner do not go with
  it, and the proxy says so at startup. `--user` and `--password` are not
  used.

## Several inputs, one catalogue or one each

Playlists and accounts of different kinds, of one provider or many, can be
served together. List them under `inputs` in the configuration file, and say
what the proxy delivers under `output`. They are not options of the command
line: they live in the file (`--iptv-proxy-config`, or `IPTV_PROXY_CONFIG` in
Docker, see [Several users](#several-users)).

```yaml
# /config/iptv-proxy.yaml
inputs:
  - name: Brazil
    type: xtream
    base-url: http://provider-one.example:8080
    user: xtream_user
    password: xtream_password
    category-prefix: BR
    individual: true          # also served alone, at /brazil/
    path: brazil
  - name: Portugal
    type: m3u
    url: http://provider-two.example/playlist.m3u   # or a file in the container
    xmltv-url: http://provider-two.example/guide.xml  # optional
    category-prefix: PT
    individual: true
    path: portugal

output:
  formats: [xtream, m3u, xmltv]
  dedupe: none                # or guide-id
  category-separator: " | "   # the default
  hdhomerun:
    - port: 5004              # every input
    - port: 5005
      sources: [Portugal]     # one input alone
```

A commented file with every option is in the repository:
[`iptv-proxy.example.yaml`](iptv-proxy.example.yaml). Copy it and edit it.

Players then use the proxy's address as always, with the combined catalogue
on `http://192.168.1.10:8080`, and `http://192.168.1.10:8080/portugal` for
the playlist alone.

### Inputs

| Field | What it does |
|---|---|
| `name` | Name in logs, on the status page and in `output.hdhomerun`. Each name is used once. |
| `type` | `xtream` or `m3u`. Required. |
| `base-url`, `user`, `password` | The Xtream Codes account (`xtream` inputs). |
| `url` | The playlist: an `http(s)` address or a local file (`m3u` inputs). |
| `xmltv-url` | The guide of an `m3u` input, when its playlist does not name one (`url-tvg`). |
| `max-connections` | Streams the proxy holds at once on the input: an input at its limit is tried last for a channel other inputs have too. The account's own limit by default (none is known for a playlist: set it). |
| `base-id` | Where the input's ids start in the combined catalogue. See [Ids](#ids). |
| `category-prefix` | Put in front of the input's categories in the combined catalogue. See [Categories](#categories). |
| `individual`, `path` | Also serve the input alone at `/<path>/` (`individual: true` needs a `path`). See [An input on its own](#an-input-on-its-own). |

With a **single input**, nothing is rewritten: an Xtream account's answers,
and its `get.php` playlist (channels, movies **and series**, with the
`type` and `output` you give it), are the provider's own. `base-id`,
`category-prefix`, `max-connections` and `individual` mean nothing then, and
the proxy refuses them.

With **several inputs**, the playlist is built from their catalogues, not
taken from a provider: it has the live channels, the movies of the playlists
and, with `--xtream-api-get-movies`, those of the Xtream accounts. **An
Xtream account's series are not in it**, and `type=m3u_plus` is not needed
(the playlist always has the attributes): only `output=ts` matters, which
gives the addresses the extension `.ts`. If you want everything an account's
own `get.php` has, give that address as an `m3u` input:

```yaml
inputs:
  - name: My provider
    type: m3u
    url: "http://provider.example/get.php?username=u&password=p&type=m3u_plus&output=ts"
    xmltv-url: "http://provider.example/xmltv.php?username=u&password=p"
```

Inputs replace `--m3u-url`, `--xtream-*`, `--xtream-passthrough` and
`sources`: they do not go together, and the proxy says so at startup.

### Playlists as inputs

An `m3u` input is read as if it were an Xtream account, so that it has the
same catalogue, ids, guide and connection limits as one, and every output
(Xtream API, playlist, tuner) can come from it.

- **Live channels and movies.** A track whose address is a file (`.mp4`,
  `.mkv`, `.avi`, `.mp3`...) is a **movie** of the input, with the container
  its extension says; any other track is a live channel. Movies are in the
  Xtream API (`get_vod_streams`) and in the playlist, with the category they
  had in yours (`Filmes | Action`). Unlike the movies of an Xtream account,
  those of a playlist are always in the playlist: they were in it to begin
  with, and `--xtream-api-get-movies` is for accounts. A playlist says
  nothing of seasons: it has no series, and a series' episodes are movies.
  An address in two groups is two entries, one in each category; the same
  address in the same group is one. A movie's `.m3u8` address cannot be told
  from a live channel's: it is a channel.
- A track's **category** is its `group-title`, its guide id its `tvg-id`.
  Movies and channels have their own lists of categories.
- A channel's **id** comes from the track's address, so it stays when the
  playlist changes, wherever the track moves. If two addresses give the same
  id (it happens in playlists of tens of thousands of tracks), the first in
  alphabetical order of address and group keeps it and the other takes the next one
  free: only tracks that collide can be affected by a change of the
  playlist.
- The playlist is read as the proxy starts (see [The startup log](#the-startup-log)),
  then again after `--m3u-cache-expiration` hours. One that cannot be read is
  kept, and asked again in five minutes.
- The guide is the `xmltv-url`, else the one the playlist names. A guide
  that cannot be read is left out of the combined guide.
- The stream is the track's address, which only the proxy follows: players
  never see it.
- **A big playlist takes memory.** The proxy keeps a playlist read, and the
  lists made from it. Plan on about 1 GB at the peak of a first read for a
  playlist of 400,000 entries (60-80 MB), and some seconds, besides the time
  the provider takes to send it. The next requests are answered from what is
  kept.
- `--xtream-passthrough` is not an input: it keeps its own options.

### Ids

In the combined catalogue every input has a range of 100,000,000 ids, and an
entry is shown as `base-id` + its id. Without `base-id`, the input at
position *n* in the list starts at *n* × 100,000,000, so the first input
keeps its ids. Players set up before see the same channels under the same
numbers.

Give a `base-id` to an input to keep its ids whatever its position: adding or
reordering inputs then changes nothing. Bases less than 100,000,000 apart,
or negative, are refused at startup.

### Categories

By default, categories of the same name at different inputs are one. A
`category-prefix` keeps them apart: the category is shown as the prefix, the
separator and the category.

| Input's category | `category-prefix: PT` | Shown |
|---|---|---|
| `News` | yes | `PT \| News` |
| *(none: a playlist's track without a group)* | yes | `PT` |
| `News` | no | `News` |

The separator is `output.category-separator`, `" | "` by default, written as
is (quote it to keep its spaces). The prefix is trimmed. Two inputs with the
same prefix share their categories. An Xtream account's channels all have a
category already: only a playlist's tracks without a group get the one of the
prefix alone.

[Filters](#filtering-channels) match the names as they are shown: with a
prefix, `^PT \|` keeps an input's categories.

### An input on its own

With `individual: true` and a `path`, an input is also served alone,
under its path: `/portugal/iptv.m3u`, `/portugal/player_api.php`,
`/portugal/xmltv.php` and its streams at `/portugal/live/<user>/<password>/<id>`.

- It shows the input's channels with **its own ids and categories**: no
  prefix, no range. Its playlist is built from the input's catalogue, as the
  combined one is: an Xtream account's series are not in it either.
- It is the same proxy: the same users and passwords, the same filters, and the
  connections are shared with the combined catalogue and with the other
  views. A stream watched in one is the same one in the other, and the limit of
  streams of an input or of a user holds for all of them.
- A view only plays its own input's channels.
- The path is made of letters, digits and `- _ . ~`. It cannot be the name of
  a user, the playlist's name, one of the proxy's own paths (`live`, `hls`,
  `status`, `get.php`...) or another view's path.
- `output.combined: false` leaves the combined catalogue out and serves the
  views only.

### Output

Without an `output` section, the proxy delivers everything its inputs allow,
as before. With one, only what it lists exists: a format left out has no
routes at all.

| Setting | What it does |
|---|---|
| `formats` | What is delivered: `xtream` (the Xtream API, `get.php`), `m3u` (the playlist), `xmltv` (the guide). All of them when it is not there; `[]` delivers none (a tuner alone). The streams exist if `xtream` or `m3u` is there. |
| `combined` | `true` by default. `false` serves the views only, and needs one. |
| `dedupe` | `none` by default: a live channel several inputs have is shown from each. `guide-id` shows it once, from the first input, and keeps the others as its fallbacks (the guide id is `epg_channel_id`, or `tvg-id` in a playlist). |
| `category-separator` | Between a category prefix and the category. |
| `hdhomerun` | The HDHomeRun tuners: see below. |

An `xtream` output needs an Xtream account or a playlist among the inputs
(a playlist is served as an Xtream account, with its channels and its movies).

A tuner is a port. Each entry has its `port`, its number of `tuners`,
its filters (`group-regex`, `channel-regex`, `group-exclude-regex`,
`channel-exclude-regex`) and optionally `sources: [name]` for one input alone;
without it, it serves every input. Ports differ from each other and from the
proxy's. `HDHOMERUN_*` options and `output.hdhomerun` do not go together.
See [Plex and other media servers](#plex-and-other-media-servers).

## Several sources

This is the first way to serve several accounts, and it keeps working. To
mix playlists and accounts, to choose what is delivered, or to serve each
input alone, see [Several inputs](#several-inputs-one-catalogue-or-one-each):
`sources` and `inputs` do not go together.

A second account, at the same provider or another, can back up the first:
list it under `sources` in the configuration file. The proxy then serves
both accounts as one catalogue.

```yaml
# /config/iptv-proxy.yaml
sources:
  - name: backup
    xtream-base-url: http://other-provider.example:8080
    xtream-user: other_user
    xtream-password: other_password
    max-connections: 1   # optional: the account's own limit by default
```

The Xtream options (`XTREAM_BASE_URL`, `XTREAM_USER`, `XTREAM_PASSWORD`)
stay the first source; without them, the first of `sources` is.

- **Ids.** The first source keeps its ids: the players set up before see
  the same channels under the same numbers. The second source's ids are
  shown as 100000000 + id, the third's as 200000000 + id, and so on.
- **Categories of the same name are one**, with the first source's id.
- **A live channel several sources have is shown once**, from the first of
  them. Channels are recognised by their guide id (`epg_channel_id`): a
  channel without one is shown from each source.
- **Failover.** When a live channel fails at its source (an error, an HTTP
  error), the proxy opens it at the next source that has it. A source
  already holding as many streams as its account allows is passed over:
  its channels play from another source while its connections are busy.
- Movies and series of every source are listed, each from its own source.
- The guide is the first source's, plus the channels only the other sources
  have. The playlist (`get.php`, `iptv.m3u`) is built from the merged
  catalogue, as with `--xtream-api-get`.
- A source that does not answer is left out of the lists for that request;
  the last complete answer is served instead when there is one.
- Sources are Xtream accounts: they do not go with `--m3u-url` or with
  `--xtream-passthrough`.

## Plex and other media servers

With `--hdhomerun-port`, the proxy also answers as an HDHomeRun network tuner
on that port. Plex, Emby, Jellyfin and Channels DVR then see your live
channels as a TV tuner, with a guide matched to them, and can record.

```sh
docker run -d --name iptv-proxy -p 8080:8080 -p 192.168.1.10:5004:5004 \
  -e XTREAM_BASE_URL="http://provider.example:8080" \
  -e XTREAM_USER=xtream_user \
  -e XTREAM_PASSWORD=xtream_password \
  -e PROXY_HOSTNAME=192.168.1.10 \
  -e PROXY_USER=family \
  -e PROXY_PASSWORD=choose-a-password \
  -e HDHOMERUN_PORT=5004 \
  -e HDHOMERUN_GROUP_REGEX='^(FR|UK) ' \
  pierro777/iptv-proxy:latest
```

In Plex: **Settings > Live TV & DVR > Set up Plex DVR**, then "Don't see your
HDHomeRun device? Enter its network address manually": `192.168.1.10:5004`.
When Plex asks for the guide, choose the XMLTV option and give
`http://192.168.1.10:5004/guide.xml`.

When Plex runs in Docker on the same machine, put both containers on one
network and give Plex `iptv-proxy:5004` and `http://iptv-proxy:5004/guide.xml`
instead: the tuner's port then needs no publishing at all.

- **A tuner has no login**: anyone who reaches its port can watch. Keep it on
  your local network: do not publish it on the Internet or behind your
  reverse proxy. In exchange, nothing the tuner answers holds your proxy's or
  your provider's credentials.
- The tuner serves the live channels the filters keep, and only those. Use
  them: a media server is not made for 10,000 channels. Besides the proxy's
  filters, the tuner has its own (`--hdhomerun-group-regex`,
  `--hdhomerun-channel-regex`, `--hdhomerun-group-exclude-regex`,
  `--hdhomerun-channel-exclude-regex`), applied after them: your players
  keep every channel while the media server gets a few groups.
- **Plex takes no more than about 400 channels** with a guide for a tuner:
  past that, adding the DVR fails at its last step ("try again later"). The
  tuner helps: a channel the provider lists several times with the same
  guide id (another quality, a backup) is one tuner channel, the first of
  the list; the others are its fallbacks, played when it fails. If Plex
  still refuses, narrow the tuner's filters.
- With an Xtream account, a channel's number is its id at the provider: it
  does not change when the provider reorders its list, so recordings stay on
  their channel. With an M3U playlist, it is the track's `tvg-chno`, else its
  place in the playlist.
- The guide at `/guide.xml` names each channel by its tuner number, which is
  how media servers match a guide to channels.
- **Channel logos in Plex.** Plex's apps are served over HTTPS and refuse a
  logo at a plain `http://` address; many providers have only those. When
  the proxy's own address is HTTPS (`--https`, see
  [Behind a reverse proxy](#behind-a-reverse-proxy-with-https)), the tuner's
  guide gives such logos through that address, where the Plex apps load them
  and the proxy fetches them. With `--proxy-logos`, all of them come through
  it.
- **Several tuners.** With `inputs` and `output.hdhomerun`, each entry is a
  tuner on a port of its own, for every input or for one of them
  (`sources: [name]`): say, one for Plex on port 5004 and one on 5005 for the
  playlist you only want to record from. Give Plex each address. A tuner
  has its own channel list, filters and number of tuners; the streams the
  proxy holds at an account are counted for all.
- The tuner announces as many tuners as your Xtream account allows
  connections, or, with several sources, as their accounts allow together
  (`--hdhomerun-tuners` to change it): a media server never opens more
  streams than that.
- Media servers expect MPEG-TS. With an Xtream account, the tuner asks the
  provider for MPEG-TS. A channel the provider has only as HLS (an M3U
  playlist of `.m3u8` addresses, or an account whose
  `allowed_output_formats` has no `ts`) plays when ffmpeg is at hand: take
  an `-ffmpeg` image (see [Docker images](#docker-images)), or have
  `ffmpeg` in the `PATH` of the binary. ffmpeg then reads the HLS stream and
  writes it as MPEG-TS, without encoding it again; viewers of a channel
  share one ffmpeg. Without ffmpeg, such a channel is passed on as a
  playlist, which media servers do not play.
- Plex finds a tuner by its address, entered by hand: automatic discovery on
  the network is not supported.

## Status page

With `--status-password` (`STATUS_PASSWORD`), the proxy serves a read-only
page at `http://host:port/status`. It asks for that password; any user name
does.

It shows:
- the streams being watched: by whom, from which address, for how long;
- the users and their limits;
- how many live streams are open at the provider, shared between their
  viewers;
- the sources and the streams each holds, when there are several;
- the HDHomeRun tuner's channels;
- the answers kept for when the provider fails.

The page refreshes itself every 10 seconds; `/status.json` gives the same
in JSON. It changes nothing, and shows no password: stream addresses are
masked, sources are named by their host. `iptv-proxy --version` prints the
version.

## The startup log

The proxy tells what it runs with, then what it is doing while it starts. In
Docker, it is what `docker logs <container>` shows first:

```
Using config file: /config/iptv-proxy.yaml
iptv-proxy 4.8.0

Hostname:       tv.example.com
Listening on:   :8080
Addresses as:   https://tv.example.com:443
User-Agent:     VLC/3.0.20 LibVLC/3.0.20
Playlist cache: 1 hour(s)
Live streams:   one provider connection shared between players

Users:
  - user1
  - user2 (2 streams at once)

Inputs:
  - Input 1 · m3u · http://provider.example (ids from 0)
  - Famelack · m3u · https://raw.githubusercontent.com (ids from 100000000)

Output:
  Formats:            m3u, xmltv
  Combined catalogue: yes
  Dedupe:             none

[iptv-proxy] Listening on :8080
[iptv-proxy] Reading "Input 1" (m3u, http://provider.example)...
[iptv-proxy] Reading "Famelack" (m3u, https://raw.githubusercontent.com)...
[iptv-proxy] "Famelack" ready: 6227 channels, 0 movies, 222 categories (115ms)
[iptv-proxy] "Input 1" ready: 125400 channels, 273600 movies, 103 categories (4.6s)
[iptv-proxy] Ready: 2 playlist(s) read in 4.6s
```

- **The summary** is the settings as the proxy understood them: a quick way
  to check a configuration file. It also lists the prefixes, the views
  (`/portugal`), the limits and filters of the users, and the HDHomeRun
  tuners, when there are any. `Listening on` is the port **inside** the
  container; `Addresses as` is what the playlists write (`https`,
  `advertised-port`).
- **It gives no secret**: users are named, never their password; an input is
  its scheme and host, never its path or query, where a provider's login
  usually is; a local file is `local file`.
- **The playlists of the `m3u` inputs are read as the proxy starts**, all at
  once, and the log tells what each holds and how long it took. A request
  that comes meanwhile waits for that same reading, and does not start
  another: the first client after a restart finds the lists read, or waits
  for what is left of the reading. `Ready` is when all of them are read.
- **A playlist that cannot be read does not stop the proxy.** It says
  `failed`, and `Ready, with 1 of 2 playlist(s) not read`; the playlist is
  read again when a client asks.
- The Xtream accounts are not asked at the start, only when a client needs
  them. The playlist of each user is built when that user first asks, and the
  log gives its size and time (`playlist of user1 built: 407282 entries,
  68.2 MB (6.1s)`); it is then kept for `--m3u-cache-expiration` hours.

## When the provider fails

Providers fail now and then: an error for a minute, a maintenance page, a
guide that will not generate. The proxy keeps the last answer the provider
gave in full for the lists players load (the login, categories, channels,
movies, series, details, playlists and the guide), compressed in memory. When
the provider fails, players get that answer instead of an error, and the log
says so. Streams themselves are not kept: when the provider is down, nothing
plays.

A refusal (wrong account, expired subscription) is passed on: it says
something you need to know.

## Filtering channels

Four options keep the live channels you want and leave out the others. Each
takes a [regular expression](https://github.com/google/re2/wiki/Syntax) matched
against a channel's group (`group-title` in a playlist, the category in the
Xtream API) or its name:

- `--group-regex`, `--channel-regex`: keep only what matches.
- `--group-exclude-regex`, `--channel-exclude-regex`: leave out what matches,
  even if it was kept by the first two.

A channel is kept when it passes all of them. The expressions are case
sensitive; start one with `(?i)` to ignore case.

```sh
  -e GROUP_REGEX='^(FR|UK) ' \
  -e GROUP_EXCLUDE_REGEX='(?i)adult|xxx' \
  -e CHANNEL_EXCLUDE_REGEX='(?i)backup|test' \
```

The filters apply to:

- the M3U playlist (`/iptv.m3u`) and the Xtream playlists (`/get.php`,
  `/apiget`): a channel left out is not in it, and in M3U mode it has no
  address on the proxy;
- the live categories and channels of the Xtream API;
- the guide (`/xmltv.php`): only the channels kept, and their programmes, are
  in it. A guide of thousands of channels shrinks to the ones you watch.

Movies and series are not filtered. In a `get.php` playlist that holds movies,
the same rules apply to them, by their group and name.

The filters also hold for streams: a live channel left out does not play,
even asked by its id, and the provider is not asked for it. A channel the
provider just added plays as soon as it passes the filters: when a stream
asks for an id the list does not have, the list is read again (at most once
a minute).

## Live streams: one connection to the provider

An IPTV account allows a few connections at a time, often a single one. When
several players watch the same live channel, the proxy opens one connection to
the provider and shares it: the first player opens the stream, the next ones
join it where it is, and the connection is closed when the last one leaves.

If the provider drops the stream, or leaves it silent for 20 seconds, the
proxy opens it again while someone is watching, so the player does not have
to reconnect. After a few attempts that fail, the stream ends.

Movies, series and catch-up are files: each player reads its own, from where
it wants. `--no-stream-sharing` gives every player its own connection for live
streams too.

## Behind a reverse proxy, with HTTPS

The proxy listens in plain HTTP. To serve it on your own domain with HTTPS,
put a reverse proxy (Traefik, Caddy, nginx...) in front, and tell iptv-proxy
which addresses to give out:

```yaml
    environment:
      PORT: 8080             # where iptv-proxy listens
      PROXY_HOSTNAME: iptv.example.com
      ADVERTISED_PORT: 443   # the port your players use
      HTTPS: 1               # addresses given out start with https://
```

Your players then use `https://iptv.example.com:443/iptv.m3u?username=...&password=...`.

The proxy's user and password travel in every address, so use HTTPS as soon as
the proxy is reachable from the Internet.

A complete example with Traefik and Let's Encrypt is in the
[`traefik`](traefik) folder. From the root of the repository:

```sh
cp -r ./traefik/* .
mkdir -p Traefik/etc/traefik Traefik/log
```

Then replace `iptv.proxyexample.xyz` with your domain in `docker-compose.yml`,
set your e-mail address in `Traefik/traefik.yaml`, and start it:

```sh
docker compose up -d
```

## What it does not do

- It does not provide channels or streams: you need a playlist or an account
  from a provider.
- It does not edit the catalogue: no channel editor, no renaming of channels,
  no guide mapping. Players get what the provider sends; what the proxy
  changes is the ids and the category names of inputs served together (see
  [Several inputs](#several-inputs-one-catalogue-or-one-each)).
- It does not transcode, record or cache streams: they are passed on as they
  come. The one exception is the tuner's HLS channels, which ffmpeg turns into
  MPEG-TS, without encoding them again.
- It has no web interface to change anything: the status page only shows.

## Roadmap

Nothing is planned beyond fixes: what comes next depends on what users ask
for. Open an issue for a bug or an idea, or a discussion for a question.

See the [changelog](CHANGELOG.md) for what each release brought.

## Contributing

Bug reports, ideas and pull requests are welcome: see
[CONTRIBUTING.md](CONTRIBUTING.md). Questions go to the
[discussions](https://github.com/pierre-emmanuelJ/iptv-proxy/discussions),
vulnerabilities to [SECURITY.md](SECURITY.md).

## License

[GPL-3.0](LICENSE).

Built with [cobra](https://github.com/spf13/cobra) and
[gin](https://github.com/gin-gonic/gin).

If the project is useful to you, you can buy me a beer:

[![paypal](https://www.paypalobjects.com/en_US/i/btn/btn_donate_LG.gif)](https://www.paypal.com/donate?hosted_button_id=WQAAMQWJPKHUN)
