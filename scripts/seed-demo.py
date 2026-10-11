#!/usr/bin/env python3
"""Superseded: the Northwind Traders demo workspace is now built into AbiData.

  * New installs get it automatically when first-run setup completes
    (disable with ABIDATA_DEMO_WORKSPACE=false).
  * On an existing install, stop the server and run:
        abidata demo --owner you@example.com
    (Docker: docker compose run --rm --no-deps --entrypoint /abidata abidata demo --owner you@example.com)

The content lives in internal/demo. This script no longer seeds anything.
"""
import sys

sys.exit(__doc__)
