#!/usr/bin/env python3
"""Run one Edge Delta v3 pipeline on the Go agent and on RED side by side and diff the data.

Both agents get a copy of the same config in which every input node is replaced
by a local receiver (http_input for line fixtures, otlp_input for OTLP/HTTP JSON
fixtures) and every output node by an http_output pointing at a capture server
in this process. The same fixture bytes are posted to both agents. After
--settle seconds both are stopped with SIGTERM, and the captured records are
normalized and compared per output node. Set --settle longer than the longest
processor interval: the Go agent was observed to exit without emitting an open
aggregation window, while RED emitted it at shutdown.

Usage:
  python3 sidebyside.py --config pipeline.yaml --go-bin ./edgedelta --red-bin ./red \
      --feed my_http_input=logs.ndjson --feed my_otlp_input=signals.otlp.json --out ./run1

Exit code 0 means every output node matched after normalization; 1 means a
diff; 2 means the harness itself failed (bad arguments, a trace fixture, an
agent that did not open its inputs, a rejected feed, or a Go agent that
captured nothing on any output).
"""
import argparse
import collections
import copy
import http.server
import json
import os
from pathlib import Path
import re
import signal
import socket
import subprocess
import sys
import threading
import time
import urllib.request

import yaml

AGENTS = ("go", "red")
# Fields dropped before comparing. Only `timestamp` differs between the agents
# when both run on one machine: http_input stamps each log with its arrival
# time. Pass --no-default-ignore when fixtures carry their own timestamps
# (OTLP), and add to it with --ignore only for documented nondeterminism.
DEFAULT_IGNORE = {"timestamp"}
# Set by a probe in front of every output on items whose timestamp is not an
# integer. The Go agent reads the timestamp as int64 when it ships an item, so
# a float timestamp (ParseJSON returns numbers as float64 there) reaches the
# backend as 0 and the log is dropped. http_output prints the
# float 1793697822400.0 as 1793697822400, so the diff alone cannot see it.
BAD_TS = "sidebyside.bad_timestamp"


def fail(msg):
    """Exit 2: the harness could not compare anything."""
    print(msg, file=sys.stderr)
    sys.exit(2)


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def is_input(t):
    return t.endswith("_input")


def is_output(t):
    return t.endswith("_output") or t in ("ed_output",)


def rewrite(cfg, agent, feeds, ports, capture_port):
    """Return this agent's copy of cfg with inputs and outputs made local."""
    c = copy.deepcopy(cfg)
    c.setdefault("settings", {})["tag"] = f"sidebyside-{agent}"
    dropped = set()
    nodes = []
    for n in c.get("nodes", []):
        name, t = n["name"], n.get("type", "")
        if is_input(t):
            if name not in feeds:
                dropped.add(name)
                continue
            port = ports[agent][name]
            if feeds[name].name.endswith(".otlp.json"):
                nodes.append({"name": name, "type": "otlp_input", "port": port, "protocol": "http"})
            else:
                nodes.append({"name": name, "type": "http_input", "port": port})
            continue
        if is_output(t):
            nodes.append({"name": name, "type": "http_output",
                          "endpoint": f"http://127.0.0.1:{capture_port}/{agent}/{name}"})
            continue
        nodes.append(n)
    links = [l for l in c.get("links", []) if l["from"].split("->")[0] not in dropped]
    for out in [n["name"] for n in nodes if n["type"] == "http_output"]:
        probe = f"sidebyside_timestamp_probe_{out}"
        nodes.append({"name": probe, "type": "sequence", "processors": [{
            "type": "ottl_transform", "data_types": ["log", "metric"],
            "statements": f'set(attributes["{BAD_TS}"], true) where not IsInt(timestamp)'}]})
        links = [dict(l, to=probe) if l["to"] == out else l for l in links] + [{"from": probe, "to": out}]
    c["nodes"] = nodes
    c["links"] = links
    return c, sorted(dropped)


class Capture(http.server.ThreadingHTTPServer):
    def __init__(self, port):
        self.bodies = collections.defaultdict(list)
        self.lock = threading.Lock()
        super().__init__(("127.0.0.1", port), CaptureHandler)


class CaptureHandler(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        body = self.rfile.read(int(self.headers.get("Content-Length") or 0))
        if self.headers.get("Content-Encoding") == "gzip":
            import gzip
            body = gzip.decompress(body)
        _, agent, node = self.path.split("/", 2)
        with self.server.lock:
            self.server.bodies[(agent, node)].append(body)
        self.send_response(200)
        self.end_headers()

    do_PUT = do_POST

    def log_message(self, *a):
        pass


def records(body):
    """Split one captured payload into records: a JSON array, one JSON object, or NDJSON."""
    text = body.decode("utf-8", "replace").strip()
    if not text:
        return []
    try:
        v = json.loads(text)
        return v if isinstance(v, list) else [v]
    except json.JSONDecodeError:
        out = []
        for line in text.splitlines():
            line = line.strip()
            if not line:
                continue
            try:
                out.append(json.loads(line))
            except json.JSONDecodeError:
                out.append({"__raw__": line})
        return out


def normalize(v, ignore, ports):
    """Drop ignored keys, parse JSON-in-string, and replace this agent's own
    listener ports with a per-input placeholder.

    The two agents must listen on different ports, and inputs stamp the port
    into server.port and service.name. That difference is made by the harness,
    so the harness removes it; the fields themselves are still compared.
    """
    if isinstance(v, dict):
        return {k: normalize(x, ignore, ports) for k, x in sorted(v.items()) if k not in ignore}
    if isinstance(v, list):
        return [normalize(x, ignore, ports) for x in v]
    if isinstance(v, int) and not isinstance(v, bool) and v in ports:
        return ports[v]
    if isinstance(v, str):
        if v[:1] in "{[":
            # A body that is itself serialized JSON is compared structurally, so
            # key order differences between encoders do not count as a diff.
            try:
                return normalize(json.loads(v), ignore, ports)
            except json.JSONDecodeError:
                pass
        for port, name in ports.items():
            # Only a whole number: a port such as 55500 also appears inside
            # timestamps like 59.555000Z.
            v = re.sub(rf"(?<!\d){port}(?!\d)", name, v)
    return v


def wait_port(port, proc, timeout):
    end = time.time() + timeout
    while time.time() < end:
        if proc.poll() is not None:
            return False
        try:
            with socket.create_connection(("127.0.0.1", port), timeout=0.5):
                return True
        except OSError:
            time.sleep(0.3)
    return False


def post(url, data, ctype):
    req = urllib.request.Request(url, data=data, headers={"Content-Type": ctype}, method="POST")
    with urllib.request.urlopen(req, timeout=30) as r:
        return r.status


def feed(agent, feeds, ports, batch):
    for name, path in feeds.items():
        port = ports[agent][name]
        if path.name.endswith(".otlp.json"):
            # One OTLP/HTTP JSON request per line; the signal comes from the top-level key.
            for line in path.read_text().splitlines():
                if not line.strip():
                    continue
                doc = json.loads(line)
                route = ("/v1/logs" if "resourceLogs" in doc else
                         "/v1/metrics" if "resourceMetrics" in doc else "/v1/traces")
                post(f"http://127.0.0.1:{port}{route}", line.encode(), "application/json")
            continue
        lines = [l for l in path.read_bytes().splitlines() if l.strip()]
        for i in range(0, len(lines), batch):
            post(f"http://127.0.0.1:{port}/", b"\n".join(lines[i:i + batch]) + b"\n", "text/plain")


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--config", type=Path, required=True, help="the Go pipeline (v3 YAML)")
    p.add_argument("--red-config", type=Path, help="the migrated RED pipeline; defaults to --config")
    p.add_argument("--go-bin", type=Path, required=True)
    p.add_argument("--red-bin", type=Path, required=True)
    p.add_argument("--feed", action="append", default=[], metavar="INPUT_NODE=FILE",
                   help="lines file -> http_input, *.otlp.json (one request per line) -> otlp_input")
    p.add_argument("--out", type=Path, required=True)
    p.add_argument("--settle", type=float, default=15, help="seconds to wait after feeding, before stopping")
    p.add_argument("--startup", type=float, default=60, help="seconds to wait for each agent's inputs")
    p.add_argument("--batch", type=int, default=100, help="lines per POST")
    p.add_argument("--ignore", action="append", default=[], help="extra field name to drop before comparing")
    p.add_argument("--no-default-ignore", action="store_true", help="compare every field, including DEFAULT_IGNORE")
    p.add_argument("--max-samples", type=int, default=5)
    p.add_argument("--pair-by", default="body",
                   help="top-level record field used to pair only_go and only_red records for value_diffs")
    a = p.parse_args()

    a.out.mkdir(parents=True, exist_ok=True)
    feeds = {}
    for f in a.feed:
        name, _, path = f.partition("=")
        feeds[name] = Path(path).resolve()
        if feeds[name].name.endswith(".otlp.json") and "resourceSpans" in feeds[name].read_text():
            # Outputs are captured through http_output, which sends no traces
            # on either agent, so spans would be missing on both sides and the
            # run would report a match it never checked.
            fail(f"{path}: traces cannot be compared by this harness; remove the resourceSpans lines")
    ignore = (set() if a.no_default_ignore else DEFAULT_IGNORE) | set(a.ignore)
    srcs = {"go": yaml.safe_load(a.config.read_text()),
            "red": yaml.safe_load((a.red_config or a.config).read_text())}
    capture_port = free_port()
    ports = {ag: {n: free_port() for n in feeds} for ag in AGENTS}
    dropped = {}
    for ag in AGENTS:
        cfg, dropped[ag] = rewrite(srcs[ag], ag, feeds, ports, capture_port)
        missing = set(feeds) - {n["name"] for n in cfg["nodes"]}
        if missing:
            fail(f"--feed names no input node in the {ag} config: {sorted(missing)}")
        (a.out / f"{ag}.yaml").write_text(yaml.safe_dump(cfg, sort_keys=False))

    srv = Capture(capture_port)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    # Inherited ED_* variables (an API key, an endpoint) would connect an agent
    # to a real org; start from none. Without ED_COORDINATOR_CLIENT_DISABLED
    # the Go agent never opens its inputs.
    env = {k: v for k, v in os.environ.items() if not k.startswith("ED_")}
    env.update(ED_BACKEND_DISABLED="1", ED_SKIP_CONF_DOWNLOAD="1", ED_DISABLE_CONF_RELOAD="1",
               ED_COORDINATOR_CLIENT_DISABLED="1", ED_AGENT_IDENTIFIER_OVERRIDE="sidebyside")
    procs = {}
    bins = {"go": [str(a.go_bin.resolve()), "-c"], "red": [str(a.red_bin.resolve()), "--config"]}
    for ag in AGENTS:
        work = (a.out / ag).resolve()
        work.mkdir(exist_ok=True)
        log = (a.out / f"{ag}.log").open("w")
        procs[ag] = subprocess.Popen(bins[ag] + [str((a.out / f"{ag}.yaml").resolve())],
                                     env=dict(env, ED_LOGGING_DIR=str(work)), cwd=work,
                                     stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
    status = "ok"
    try:
        for ag in AGENTS:
            for name, port in ports[ag].items():
                if not wait_port(port, procs[ag], a.startup):
                    status = f"{ag} did not open input {name} on :{port}; see {a.out / ag} and {a.out / (ag + '.log')}"
                    raise RuntimeError(status)
        threads = [threading.Thread(target=feed, args=(ag, feeds, ports, a.batch)) for ag in AGENTS]
        [t.start() for t in threads]
        [t.join() for t in threads]
        time.sleep(a.settle)
    except Exception as e:  # noqa: BLE001 - reported, not swallowed
        status = status if status != "ok" else f"harness error: {e}"
    finally:
        # SIGTERM first so each agent can shut down normally; SIGKILL only
        # after 60 seconds.
        for pr in procs.values():
            if pr.poll() is None:
                os.killpg(pr.pid, signal.SIGTERM)
        for pr in procs.values():
            try:
                pr.wait(timeout=60)
            except subprocess.TimeoutExpired:
                os.killpg(pr.pid, signal.SIGKILL)
        time.sleep(1)
        srv.shutdown()

    outputs = sorted({n["name"] for n in srcs["go"]["nodes"] if is_output(n.get("type", ""))})
    report = {"status": status, "dropped_inputs": dropped, "feeds": {k: str(v) for k, v in feeds.items()},
              "ignored_fields": sorted(ignore), "outputs": {}}
    all_match = status == "ok"
    for node in outputs:
        raw = {ag: [r for b in srv.bodies.get((ag, node), []) for r in records(b)] for ag in AGENTS}
        bad_ts = {ag: sum(1 for r in raw[ag] if isinstance(r, dict)
                          and (r.get("attributes") or {}).pop(BAD_TS, None) is not None)
                  for ag in AGENTS}
        got = {ag: [normalize(r, ignore, {pt: f"<port:{n}>" for n, pt in ports[ag].items()}) for r in raw[ag]]
               for ag in AGENTS}
        (a.out / f"captured-{node}.json").write_text(json.dumps(got, indent=1, sort_keys=True))
        keys = {ag: collections.Counter(json.dumps(r, sort_keys=True) for r in got[ag]) for ag in AGENTS}
        only_go = keys["go"] - keys["red"]
        only_red = keys["red"] - keys["go"]
        fields = {ag: sorted({k for r in got[ag] if isinstance(r, dict) for k in flat_keys(r)}) for ag in AGENTS}
        entry = {"go_count": len(got["go"]), "red_count": len(got["red"]),
                 "match": status == "ok" and not only_go and not only_red and not any(bad_ts.values()),
                 "bad_timestamp": bad_ts,
                 "only_go": sum(only_go.values()), "only_red": sum(only_red.values()),
                 "fields_only_go": sorted(set(fields["go"]) - set(fields["red"])),
                 "fields_only_red": sorted(set(fields["red"]) - set(fields["go"])),
                 "sample_only_go": [json.loads(k) for k in list(only_go)[:a.max_samples]],
                 "sample_only_red": [json.loads(k) for k in list(only_red)[:a.max_samples]],
                 "value_diffs": value_diffs(only_go, only_red, a.pair_by)}
        report["outputs"][node] = entry
        all_match &= entry["match"]
    if status == "ok" and feeds and all(e["go_count"] == 0 for e in report["outputs"].values()):
        # Go swaps an output it cannot build for a no-op pusher and only logs
        # an error, so "nothing from Go" is a harness failure, not a pass.
        report["status"] = status = "go captured nothing on any output; check go/*.log for a no-op pusher"
    (a.out / "report.json").write_text(json.dumps(report, indent=2, sort_keys=True))
    print(json.dumps({"status": report["status"], "dropped_inputs": dropped,
                      "outputs": {k: {x: v[x] for x in ("go_count", "red_count", "only_go", "only_red", "match", "bad_timestamp",
                                                         "fields_only_go", "fields_only_red")}
                                  | {"value_diffs": v["value_diffs"]["fields"][:a.max_samples]}
                                  for k, v in report["outputs"].items()}}, indent=2))
    print(f"report: {a.out / 'report.json'}")
    sys.exit(2 if status != "ok" else 0 if all_match else 1)


def value_diffs(only_go, only_red, pair_by):
    """Pair the unmatched records on pair_by and count the fields whose values differ.

    Records that share a pair_by value are paired with the closest one, so a
    repeated body does not pair unrelated records. When every record differs,
    this usually names the one field responsible.
    """
    pool = collections.defaultdict(list)
    for k in only_red.elements():
        r = json.loads(k)
        pool[json.dumps(r.get(pair_by) if isinstance(r, dict) else r, sort_keys=True)].append(r)
    fields, paired, unpaired = collections.Counter(), 0, 0
    example = {}
    for k in only_go.elements():
        g = json.loads(k)
        cands = pool.get(json.dumps(g.get(pair_by) if isinstance(g, dict) else g, sort_keys=True))
        if not cands:
            unpaired += 1
            continue
        fg = flat_items(g)
        diffs = [[f for f in set(fg) | set(fr) if fg.get(f, "<absent>") != fr.get(f, "<absent>")]
                 for fr in (flat_items(r) for r in cands)]
        best = min(range(len(cands)), key=lambda i: len(diffs[i]))
        fr = flat_items(cands.pop(best))
        paired += 1
        for f in diffs[best]:
            fields[f] += 1
            example.setdefault(f, (fg.get(f, "<absent>"), fr.get(f, "<absent>")))
    return {"pair_by": pair_by, "paired": paired, "unpaired_go": unpaired,
            "unpaired_red": sum(len(v) for v in pool.values()),
            "fields": [{"field": f, "count": n, "go": example[f][0], "red": example[f][1]}
                       for f, n in fields.most_common()]}


def flat_items(d, prefix=""):
    if not isinstance(d, dict):
        return {prefix[:-1] or "<record>": d}
    out = {}
    for k, v in d.items():
        if isinstance(v, dict) and v:
            out.update(flat_items(v, f"{prefix}{k}."))
        else:
            out[f"{prefix}{k}"] = v
    return out


def flat_keys(d, prefix=""):
    for k, v in d.items():
        key = f"{prefix}{k}"
        if isinstance(v, dict) and v:
            yield from flat_keys(v, key + ".")
        else:
            yield key


if __name__ == "__main__":
    main()
