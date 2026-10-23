# switchboard

The only reader of a Telegram bot shared by several services. Telegram hands each update to a
single `getUpdates` caller, so switchboard reads them all and passes each one to the service it
belongs to. The services keep answering Telegram themselves with the same token.

Go, no dependencies, CalVer tag on every push to main.

## Routing

Every service owns a prefix, and its commands and button data start with it.

| Update | Goes to |
| :--- | :--- |
| `/wallapop_estado`, `/tv_list@bot 30` | The service whose prefix the command starts with |
| A command in a photo caption | Same, read from the caption |
| A button press | The service whose prefix the button data starts with |
| Anything else (a photo, plain text) | The fallback service, if one is set; dropped otherwise |
| A command no service owns | Dropped |

Forwarding is `POST <service>/telegram/update` with the update's JSON as Telegram sent it. A
service should answer at once and do slow work afterwards; switchboard waits 15 seconds and does
not retry.

## Menu

Every 5 minutes switchboard reads `GET <service>/telegram/commands` from each service
(`[{"command": "...", "description": "..."}]`) and publishes them all as the bot's menu when
something changed.

## Config

| Variable | |
| :--- | :--- |
| `SWITCHBOARD_TELEGRAM_TOKEN` | Bot token |
| `SWITCHBOARD_ROUTES` | `prefix=url` pairs: `wallapop_=http://wallapop-catalogue:8000,tv_=http://pixasso:8080` |
| `SWITCHBOARD_FALLBACK` | The prefix that takes updates with no command, like photos |
| `SWITCHBOARD_PORT` | `/healthz`, default 8000 |
| `SWITCHBOARD_LOG_JSON` | `1` for JSON logs |
