#!/usr/bin/env python3
from __future__ import annotations

import argparse
from pathlib import Path

from runtime_provider_contract import ContractError, check_contract_snapshot, sync_contract_snapshot


def main() -> int:
    parser = argparse.ArgumentParser(description="Synchronize the frozen Runtime Provider host contract")
    parser.add_argument("action", choices=("sync", "check"))
    parser.add_argument("--runtime-root", required=True, type=Path)
    arguments = parser.parse_args()
    try:
        if arguments.action == "sync":
            sync_contract_snapshot(arguments.runtime_root)
        else:
            check_contract_snapshot(arguments.runtime_root)
    except (ContractError, OSError, ValueError) as error:
        parser.exit(1, f"RUNTIME_PROVIDER_CONTRACT_INVALID: {error}\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
