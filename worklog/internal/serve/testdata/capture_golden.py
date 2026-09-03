#!/usr/bin/env python3
"""One-time golden capture: run server.py's _all_tasks() over testdata/corpus
and write golden_tasks.json. Normalizations (mirrored by the Go test):
version/generated -> 0, mtime -> 0, error text -> "<any>" (unfrozen).
Regeneration requires devboard/server.py, which dies at cutover — the golden
then stands as the frozen record."""
import importlib.util, json, os, sys

here = os.path.dirname(os.path.abspath(__file__))
os.environ["DEVBOARD_DATA"] = os.path.join(here, "corpus", "data")
os.environ["DEVBOARD_WORKLOG"] = os.path.join(here, "corpus", "worklog")
server_py = sys.argv[1]
spec = importlib.util.spec_from_file_location("server", server_py)
server = importlib.util.module_from_spec(spec)
spec.loader.exec_module(server)

payload = json.loads(json.dumps(server._all_tasks(), default=str))
payload["version"] = 0
payload["generated"] = 0
for repo in payload["repos"]:
    for t in repo["tasks"]:
        if "mtime" in t:
            t["mtime"] = 0
        if "error" in t:
            t["error"] = "<any>"
out = os.path.join(here, "golden_tasks.json")
with open(out, "w") as fh:
    json.dump(payload, fh, indent=1, sort_keys=True)
    fh.write("\n")
print("wrote", out)
