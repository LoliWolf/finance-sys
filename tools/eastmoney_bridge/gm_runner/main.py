from __future__ import annotations

from bridge_api.config import load_config
from bridge_api.db import Database
from gm.api import MODE_LIVE, run

from . import strategy


def main() -> None:
    config = load_config()
    database = Database(config.sqlite_path)
    strategy.configure(config, database)
    run(
        strategy_id=config.strategy_id,
        filename="gm_runner/strategy.py",
        mode=MODE_LIVE,
        token=config.token,
    )


if __name__ == "__main__":
    main()
