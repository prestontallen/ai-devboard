#!/usr/bin/env python3
"""Devboard: read-only dashboard over a directory of task files.

Layout: <DEVBOARD_DATA>/<repo>/<task>.{yaml,yml,json}
Endpoints:
  /            -> static/index.html
  /api/tasks   -> all tasks, parsed, grouped by repo dir
  /events      -> SSE stream; emits a message whenever the data dir changes
"""

import json
import os
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import yaml

DATA_DIR = os.environ.get("DEVBOARD_DATA", "/data")
PORT = int(os.environ.get("DEVBOARD_PORT", "8484"))
SCAN_INTERVAL = float(os.environ.get("DEVBOARD_SCAN_INTERVAL", "1.0"))
STATIC_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), "static")
TASK_EXTS = (".yaml", ".yml", ".json")

_version = 0
_changed = threading.Condition()


def _snapshot():
    """Map of task-file path -> (mtime, size) for change detection."""
    snap = {}
    try:
        with os.scandir(DATA_DIR) as repos:
            for repo in repos:
                if not repo.is_dir() or repo.name.startswith("."):
                    continue
                with os.scandir(repo.path) as files:
                    for f in files:
                        if f.is_file() and f.name.lower().endswith(TASK_EXTS):
                            st = f.stat()
                            snap[f.path] = (st.st_mtime_ns, st.st_size)
    except FileNotFoundError:
        pass
    return snap


def _watcher():
    global _version
    prev = _snapshot()
    while True:
        time.sleep(SCAN_INTERVAL)
        cur = _snapshot()
        if cur != prev:
            prev = cur
            with _changed:
                _version += 1
                _changed.notify_all()


def _parse_task(path):
    rel = os.path.relpath(path, DATA_DIR)
    entry = {"file": rel, "id": os.path.splitext(os.path.basename(path))[0]}
    try:
        with open(path, "r", encoding="utf-8", errors="replace") as fh:
            raw = fh.read()
        data = json.loads(raw) if path.lower().endswith(".json") else yaml.safe_load(raw)
        if not isinstance(data, dict):
            raise ValueError("top level must be a mapping")
        entry["task"] = data
        st = os.stat(path)
        entry["mtime"] = st.st_mtime
    except Exception as exc:  # any bad file becomes an error card, never a 500
        entry["error"] = f"{type(exc).__name__}: {exc}"
    return entry


def _all_tasks():
    repos = []
    try:
        names = sorted(
            d for d in os.listdir(DATA_DIR)
            if not d.startswith(".") and os.path.isdir(os.path.join(DATA_DIR, d))
        )
    except FileNotFoundError:
        names = []
    for name in names:
        rdir = os.path.join(DATA_DIR, name)
        tasks = [
            _parse_task(os.path.join(rdir, f))
            for f in sorted(os.listdir(rdir))
            if f.lower().endswith(TASK_EXTS) and os.path.isfile(os.path.join(rdir, f))
        ]
        if tasks:
            repos.append({"repo": name, "tasks": tasks})
    with _changed:
        v = _version
    return {"version": v, "generated": time.time(), "repos": repos}


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt, *args):
        pass

    def _send(self, code, body, ctype="application/json"):
        data = body if isinstance(body, bytes) else body.encode("utf-8")
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(data)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        path = self.path.split("?", 1)[0]
        if path in ("/", "/index.html"):
            with open(os.path.join(STATIC_DIR, "index.html"), "rb") as fh:
                self._send(200, fh.read(), "text/html; charset=utf-8")
        elif path == "/api/tasks":
            # default=str: YAML yields dates/times json can't serialize natively
            self._send(200, json.dumps(_all_tasks(), default=str))
        elif path == "/events":
            self._sse()
        else:
            self._send(404, '{"error": "not found"}')

    def _sse(self):
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        with _changed:
            last = _version
        try:
            self.wfile.write(f"data: {{\"version\": {last}}}\n\n".encode())
            self.wfile.flush()
            while True:
                with _changed:
                    if _version == last:
                        _changed.wait(timeout=15.0)
                    cur = _version
                if cur != last:
                    last = cur
                    self.wfile.write(f"data: {{\"version\": {cur}}}\n\n".encode())
                else:
                    self.wfile.write(b": keepalive\n\n")
                self.wfile.flush()
        except (BrokenPipeError, ConnectionResetError):
            pass


def main():
    threading.Thread(target=_watcher, daemon=True).start()
    server = ThreadingHTTPServer(("0.0.0.0", PORT), Handler)
    print(f"devboard: serving {DATA_DIR} on http://0.0.0.0:{PORT}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
