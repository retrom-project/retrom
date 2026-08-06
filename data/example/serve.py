#!/usr/bin/env python3
"""Serve the repository with the isolation headers required by threaded cores."""

from __future__ import annotations

import argparse
import json
import time
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path


DEFAULT_ROOT = Path(__file__).resolve().parents[2]


class RetromHandler(SimpleHTTPRequestHandler):
    server_version = "RetromExampleServer/1"

    def end_headers(self) -> None:
        self.send_header("Cross-Origin-Opener-Policy", "same-origin")
        self.send_header("Cross-Origin-Embedder-Policy", "require-corp")
        self.send_header("Cross-Origin-Resource-Policy", "same-origin")
        self.send_header("Cache-Control", "no-store")
        super().end_headers()

    def do_GET(self) -> None:  # noqa: N802 - inherited API
        if self.path == "/__health":
            payload = json.dumps(
                {"status": "ok", "nowMs": int(time.time() * 1000)},
                separators=(",", ":"),
            ).encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
            return
        super().do_GET()

    def log_message(self, format: str, *args: object) -> None:
        if getattr(self.server, "verbose", False):
            super().log_message(format, *args)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=4173)
    parser.add_argument("--root", type=Path, default=DEFAULT_ROOT)
    parser.add_argument("--verbose", action="store_true")
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    root = args.root.resolve(strict=True)

    def handler(*handler_args: object, **handler_kwargs: object) -> RetromHandler:
        return RetromHandler(*handler_args, directory=str(root), **handler_kwargs)

    server = ThreadingHTTPServer((args.host, args.port), handler)
    server.verbose = args.verbose  # type: ignore[attr-defined]
    print(f"Serving {root} at http://{args.host}:{args.port}", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
