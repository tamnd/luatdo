# Deploying a campaign

`luatdo` is a single static binary with no runtime dependencies.
The whole state of a machine is one data directory plus one routes file, so a host is set up by copying two things and starting a timer.

This is about running the pipeline on a host.
To install the tool and read the published graph without running anything, see the install section of the top level README, or run `luatdo neo4j install`.

## Linux

```sh
curl -fsSL https://raw.githubusercontent.com/tamnd/luatdo/main/install.sh | sh -s -- --bin-dir=/tmp/luatdo-install
sudo install -m 0755 /tmp/luatdo-install/luatdo /usr/local/bin/luatdo

sudo useradd --system --home /var/lib/luatdo --shell /usr/sbin/nologin luatdo
sudo install -d -o luatdo -g luatdo /var/lib/luatdo
sudo install -d /etc/luatdo

luatdo doctor --suggest-routes
sudo tee /etc/luatdo/routes.json > /dev/null    # paste the template, fill in the endpoints
sudo chmod 0640 /etc/luatdo/routes.json
sudo chown root:luatdo /etc/luatdo/routes.json

sudo install -m 0644 deploy/luatdo.service deploy/luatdo.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now luatdo.timer
```

Secrets go in `/etc/luatdo/env` as `NAME=value` lines, one per API key named by `api_key_env` in the routes file.
The unit reads it with `EnvironmentFile=-`, so the file is optional and a subscription endpoint that needs no key needs no file.

Check on a host:

```sh
luatdo doctor                    # routes, ontology, queue depth, Neo4j
systemctl list-timers luatdo     # when the next run starts
journalctl -u luatdo -f          # the per provision reporting lines
sudo systemctl start luatdo      # run now instead of waiting
sudo systemctl stop luatdo       # drain: finish what is in flight, start nothing new
```

`stop` sends SIGTERM, which the campaign reads as drain.
Provisions already in flight finish and are committed, and everything else stays queued.
`TimeoutStopSec` in the unit is the budget for that drain, after which systemd sends SIGKILL and the unfinished provisions are simply still in the queue next time.

## Windows

```powershell
irm https://raw.githubusercontent.com/tamnd/luatdo/main/install.ps1 | iex
.\deploy\luatdo-task.ps1
```

The installer puts the binary under `%LOCALAPPDATA%\luatdo\bin` and adds that to the user PATH, so none of this needs an administrator.
A scheduled task that has to run as a service account wants the binary somewhere that account can read instead, which means copying it to `C:\Program Files\luatdo` by hand.

The script registers a scheduled task that runs `luatdo doctor` first and starts the campaign only if a route answers, which is the same guard the systemd unit uses.
One difference is worth knowing: `Stop-ScheduledTask` kills the process rather than asking it to drain.
Nothing is corrupted, because a provision is committed only after it is complete, but the provisions that were in flight are lost work and come back in the next queue.
To drain a Windows run, press Ctrl+C in a console run instead.

## What a host holds

| Path | What it is |
| --- | --- |
| `/var/lib/luatdo` or `C:\ProgramData\luatdo` | The data directory: raw bytes, parsed documents, jobs, review queue, trusted store |
| `/etc/luatdo/routes.json` | Named endpoints in rank order, with rate cards |
| `/etc/luatdo/env` | API keys, one per `api_key_env` |
| `<data>/campaign/*.json` | One summary per run: what it did, what it spent, how long it took |

Nothing on a host is authoritative except the data directory, and everything in the data directory is rebuildable from the pinned dataset revisions.
A host can be thrown away and rebuilt, which is the point.
