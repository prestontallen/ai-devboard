#!/usr/bin/env python3
"""Devboard: dashboard over a directory of task files.

Layout: <DEVBOARD_DATA>/<repo>/<task>.{yaml,yml,json}
        <DEVBOARD_DATA>/<repo>/_archive/<task>.yaml   (archived; flagged in API)
Endpoints:
  /               -> static/index.html
  /api/tasks      -> all tasks, parsed, grouped by repo dir
  /events         -> SSE stream; emits a message whenever the data dir changes
  /api/archive    -> POST {repo, id}: move task file into <repo>/_archive/
  /api/unarchive  -> POST {repo, id}: move it back

The two POST endpoints are the server's only writes, and both are a single
validated rename — task content and the worklog dir are never touched.
"""

import json
import os
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import yaml

DATA_DIR = os.environ.get("DEVBOARD_DATA", "/data")
WORKLOG_DIR = os.environ.get("DEVBOARD_WORKLOG", "/worklog")  # notes/<id>.md rendered per task
PORT = int(os.environ.get("DEVBOARD_PORT", "8484"))
SCAN_INTERVAL = float(os.environ.get("DEVBOARD_SCAN_INTERVAL", "1.0"))
STATIC_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), "static")
TASK_EXTS = (".yaml", ".yml", ".json")
ARCHIVE_DIR = "_archive"  # per-repo subdir; the only place the server ever moves files

_version = 0
_changed = threading.Condition()


def _snapshot():
    """Map of watched-file path -> (mtime, size) for change detection.
    Covers task files and worklog notes, so note edits hot-reload too."""
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
                try:
                    with os.scandir(os.path.join(repo.path, ARCHIVE_DIR)) as arc:
                        for f in arc:
                            if f.is_file() and f.name.lower().endswith(TASK_EXTS):
                                st = f.stat()
                                snap[f.path] = (st.st_mtime_ns, st.st_size)
                except (FileNotFoundError, NotADirectoryError):
                    pass
    except FileNotFoundError:
        pass
    try:
        with os.scandir(os.path.join(WORKLOG_DIR, "notes")) as notes:
            for f in notes:
                if f.is_file() and f.name.lower().endswith(".md"):
                    st = f.stat()
                    snap[f.path] = (st.st_mtime_ns, st.st_size)
    except (FileNotFoundError, NotADirectoryError, PermissionError):
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


def _parse_task(path, archived=False):
    rel = os.path.relpath(path, DATA_DIR)
    entry = {"file": rel, "id": os.path.splitext(os.path.basename(path))[0]}
    if archived:
        entry["archived"] = True
    try:
        with open(path, "r", encoding="utf-8", errors="replace") as fh:
            raw = fh.read()
        data = json.loads(raw) if path.lower().endswith(".json") else yaml.safe_load(raw)
        if not isinstance(data, dict):
            raise ValueError("top level must be a mapping")
        entry["task"] = data
        st = os.stat(path)
        entry["mtime"] = st.st_mtime
        wl = data.get("worklog")
        if isinstance(wl, str) and wl and "/" not in wl and ".." not in wl:
            np = os.path.join(WORKLOG_DIR, "notes", wl + ".md")
            try:
                with open(np, "r", encoding="utf-8", errors="replace") as nf:
                    entry["notes"] = nf.read()
            except OSError:
                pass  # no notes file: section simply absent
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
        arcdir = os.path.join(rdir, ARCHIVE_DIR)
        if os.path.isdir(arcdir):
            tasks += [
                _parse_task(os.path.join(arcdir, f), archived=True)
                for f in sorted(os.listdir(arcdir))
                if f.lower().endswith(TASK_EXTS) and os.path.isfile(os.path.join(arcdir, f))
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
        elif path in ("/api/archive", "/api/unarchive"):
            self._send(405, '{"error": "POST only"}')
        else:
            self._send(404, '{"error": "not found"}')

    def do_POST(self):
        path = self.path.split("?", 1)[0]
        if path == "/api/archive":
            self._move(to_archive=True)
        elif path == "/api/unarchive":
            self._move(to_archive=False)
        else:
            self._send(404, '{"error": "not found"}')

    def _move(self, to_archive):
        """Rename a task file into or out of <repo>/_archive/ — the server's
        only write. The strict application/json requirement doubles as the
        CSRF guard: cross-origin JSON needs a preflight this server never
        answers, and simple-request content types are rejected here."""
        global _version
        ctype = self.headers.get("Content-Type", "").split(";")[0].strip().lower()
        if ctype != "application/json":
            return self._send(415, '{"error": "Content-Type must be application/json"}')
        try:
            length = int(self.headers.get("Content-Length", "0"))
            body = json.loads(self.rfile.read(length) or b"")
            repo, task_id = body.get("repo"), body.get("id")
        except (ValueError, AttributeError):
            return self._send(400, '{"error": "invalid JSON body"}')
        for part in (repo, task_id):
            if (not isinstance(part, str) or not part or part.startswith(".")
                    or ".." in part or any(c in part for c in "/\\")):
                return self._send(400, '{"error": "invalid repo or id"}')
        repo_dir = os.path.join(DATA_DIR, repo)
        arc_dir = os.path.join(repo_dir, ARCHIVE_DIR)
        src_dir, dst_dir = (repo_dir, arc_dir) if to_archive else (arc_dir, repo_dir)
        fname = None
        if os.path.isdir(src_dir):
            for f in sorted(os.listdir(src_dir)):
                if (f.lower().endswith(TASK_EXTS) and os.path.splitext(f)[0] == task_id
                        and os.path.isfile(os.path.join(src_dir, f))):
                    fname = f
                    break
        if not fname:
            return self._send(404, '{"error": "task not found"}')
        dst = os.path.join(dst_dir, fname)
        if os.path.exists(dst):
            return self._send(409, '{"error": "destination already exists"}')
        try:
            os.makedirs(dst_dir, exist_ok=True)
            os.rename(os.path.join(src_dir, fname), dst)
        except OSError as exc:
            return self._send(500, json.dumps({"error": f"move failed: {exc}"}))
        with _changed:  # wake SSE clients now; don't wait out the scan interval
            _version += 1
            _changed.notify_all()
        self._send(200, json.dumps(
            {"status": "archived" if to_archive else "restored", "repo": repo, "id": task_id}))

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
