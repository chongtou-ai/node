# Fix: machine-mode `sync.user.delta` remove not applied

## Symptom

Panel pushes:

```json
{"action":"remove","users":[{"id":1}],"node_id":1}
```

Node logs only:

```text
ws sync user delta event received
```

and never:

```text
users delta: remove, 1 users
users removed: -N
```

Authorized users remain connectable.

## Root cause

In **machine mode**, WS deltas are routed through `NodeMailbox` and previously drained only as a coalesced `sync.users` snapshot. That path could miss kernel hot-remove (`RemoveUsers`). Additionally:

- Early deltas before `SeedBaseline` were discarded (only `needsReconcile`), and REST reconcile could hit **304** with a stale ETag.
- `applyUserDelta` `remove` returned silently when the kernel was not running, without updating `lastUsers`.
- `RemoveUsers` returning 0 had no `UpdateUsers` fallback.

## Changes

1. `internal/controlplane/mailbox.go` — buffer early deltas, replay on seed, expose `HasDelta` / `DeltaUsers` on drain.
2. `internal/service/service.go` — prefer `EventSyncUserDelta` when draining mailbox; harden remove path; reset ETags on reconcile.
3. `internal/controlplane/{machine,panel}.go` — `ResetETags()` for forced full REST pull.
4. `internal/machine/machine.go` — warn (not debug) when WS events are dropped.

## Build / install

```bash
# Place under /www/wwwroot (requires root; agent sandbox cannot write there)
/www/wwwroot/aaa.com_7001/storage/Xboard-Node/COPY-TO-WWWROOT.sh

cd /www/wwwroot/Xboard-Node
go test ./internal/controlplane/ ./internal/service/
go build -o /usr/local/bin/xboard-node ./cmd/xboard-node
xbctl restart
```
