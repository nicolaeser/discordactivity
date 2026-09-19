# DiscordActivity

Self-hosted Discord presence host. It keeps one or more user tokens connected to the Discord gateway and publishes status and rich presence (playing, streaming, listening, watching, custom, competing).

The dashboard password is also the API key.

## Docker

After CI publishes from this repo, Compose pulls:

```text
ghcr.io/nicolaeser/discordactivity:latest
```

```bash
docker compose up -d
```

Open [http://127.0.0.1:8080](http://127.0.0.1:8080) and sign in with `DASHBOARD_PASSWORD`.

Build locally:

```bash
docker compose -f docker-compose.dev.yml up -d --build
```

## Environment

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | Listen port. Number only, no colon. |
| `DASHBOARD_PASSWORD` | _(required)_ | Dashboard login and API key. |
| `API_KEY` | — | Used if `DASHBOARD_PASSWORD` is empty. |
| `SQLITE_PATH` | `data.sqlite` | SQLite file. In the image: `/var/lib/discordactivity/data.sqlite`. |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error`. |

## API

Auth (any one):

- `Authorization: Bearer <DASHBOARD_PASSWORD>`
- `X-API-Key: <DASHBOARD_PASSWORD>`
- Session cookie from `POST /login`

| Method | Path | Description |
|---|---|---|
| `GET` | `/health` | Liveness. Always `200` if the HTTP server is up. |
| `POST` | `/login` | Exchange the password for a session cookie. |
| `GET` | `/api/v1/accounts` | List accounts and runtime status. |
| `POST` | `/api/v1/accounts` | Create an account. |
| `GET` | `/api/v1/accounts/{id}` | Get one account. |
| `PUT` | `/api/v1/accounts/{id}` | Update presence. |
| `DELETE` | `/api/v1/accounts/{id}` | Delete the account and stop the session. |
| `POST` | `/api/v1/accounts/{id}/start` | Enable and connect. |
| `POST` | `/api/v1/accounts/{id}/stop` | Disable and disconnect. |
| `POST` | `/api/v1/accounts/{id}/reconnect` | Close the live session and identify again. |

```bash
export BASE="http://127.0.0.1:8080"
export KEY="$DASHBOARD_PASSWORD"
export ID="1"

curl -sS -X POST "$BASE/api/v1/accounts/$ID/start" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{}'

curl -sS -X POST "$BASE/api/v1/accounts/$ID/stop" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{}'

curl -sS -X POST "$BASE/api/v1/accounts/$ID/reconnect" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{}'
```

Unversioned `/api/accounts` routes work the same way.

## Build from source

Requires Go 1.25+.

```bash
go build -o discordactivity ./cmd/discordactivity
PORT=8080 DASHBOARD_PASSWORD=change-me ./discordactivity
go test ./...
```
