# Eastmoney simulation Bridge

Windows-only adapter for the Eastmoney `gm.api` simulation account. The API
process owns HTTPS, HMAC verification, SQLite/WAL and callback delivery. The
Runner is the only process allowed to call `gm.api`; it executes commands
serially inside the SDK event loop. Both processes load all runtime settings
from the `expert_trade` Nacos document. `bootstrap.env` contains only
`NACOS_SERVER_ADDR`.

The SQLite and MySQL kill switches are initialized to enabled. Account
discovery, token verification and reconciliation must complete before any
operator can disable them.

`scripts/windows/start_runner.cmd` is a lightweight supervisor: if the GM
session returns after a market session or a transient SDK disconnect, it
restarts the Runner after five seconds. The Runner remains fail-closed and
never bypasses the Bridge kill switch, account check, token check or
simulation-only validation.
