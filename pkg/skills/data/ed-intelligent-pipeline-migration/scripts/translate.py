#!/usr/bin/env python3
"""Translate the legacy Go nodes of an Edge Delta v3 pipeline into nodes RED runs.

RED runs OTTL nodes only. Each legacy node and CEL form listed below maps to a
fixed rewrite that keeps the Go agent's behaviour; every other form raises an
error naming the node, and nothing is written. The translator never guesses.

  log_transform, generic_transform  -> ottl_transform
  resource_transform                -> route_ottl + <name>__apply ottl_transform
  regex_filter                      -> ottl_filter
  log_level_detector                -> ottl_transform
  parse_json_attributes             -> ottl_transform
  route (CEL)                       -> route_ottl
  log_to_pattern                    -> sequence [log_to_pattern_metric]
  log_to_metric                     -> sequence [ottl_filter, extract_metric, aggregate_metric]
  output_transform                  -> removed when it only feeds ed_output

Whole config: `//` comment lines are stripped from OTTL statements, and
`lookup` event_field paths are converted to OTTL, with those lookups wrapped in
a sequence.

Usage:
  python3 translate.py go.yaml red.yaml

Prints a JSON summary to stdout. Exits 1 with `<node>: unsupported <what>:
<expression>` on stderr when a node cannot be translated.
"""
import collections
import json
import re
import sys

import yaml

ROOTS = ("attributes", "resource", "body")
TOKEN = re.compile(r'\s*(?:(?P<str>"(?:[^"\\]|\\.)*")|(?P<op>==|!=|&&|\|\||[\[\]().!?:,])|(?P<id>[A-Za-z_][A-Za-z0-9_]*))')
LOG_TO_PATTERN_KEYS = ("num_of_clusters", "samples_per_cluster", "reporting_frequency", "drain_tree_depth", "similarity_threshold")


class Unsupported(Exception):
    """A node form outside the translation rules."""


def q(s):
    return json.dumps(s, ensure_ascii=False)


def unsupported(node, what, expr):
    return Unsupported(f"{node}: unsupported {what}: {expr}")


def tokenize(node, what, src):
    toks, i, src = [], 0, src.strip()
    while i < len(src):
        m = TOKEN.match(src, i)
        if not m or m.end() == i:
            raise unsupported(node, what, src)
        i = m.end()
        if m.group("str"):
            try:
                toks.append(("str", json.loads(m.group("str"))))
            except ValueError:
                raise unsupported(node, what, src)
        elif m.group("op"):
            toks.append(("op", m.group("op")))
        elif m.group("id"):
            toks.append(("id", m.group("id")))
    return toks


class Parser:
    """Parses the CEL subset used by legacy nodes into tuples."""

    def __init__(self, node, what, src):
        self.node, self.what, self.src = node, what, " ".join(src.split())
        self.toks, self.i = tokenize(node, what, src), 0

    def fail(self):
        return unsupported(self.node, self.what, self.src)

    def peek(self, k=0):
        return self.toks[self.i + k] if self.i + k < len(self.toks) else (None, None)

    def take(self, kind, val=None):
        t = self.peek()
        if t[0] != kind or (val is not None and t[1] != val):
            raise self.fail()
        self.i += 1
        return t[1]

    def accept(self, val):
        if self.peek() == ("op", val):
            self.i += 1
            return True
        return False

    def parse(self):
        e = self.ternary()
        if self.i != len(self.toks):
            raise self.fail()
        return e

    def ternary(self):
        c = self.binary(0)
        if not self.accept("?"):
            return c
        t = self.ternary()
        self.take("op", ":")
        return ("cond", c, t, self.ternary())

    def binary(self, level):
        ops = (("||", "or"), ("&&", "and"), ("==", "eq", "!=", "ne", "in", "in"))
        if level == len(ops):
            return self.unary()
        e = self.binary(level + 1)
        while True:
            t = self.peek()
            names = dict(zip(ops[level][::2], ops[level][1::2]))
            if t[1] not in names or (t[0] == "str"):
                return e
            self.i += 1
            e = (names[t[1]], e, self.binary(level + 1))

    def unary(self):
        if self.accept("!"):
            return ("not", self.unary())
        return self.postfix(self.primary())

    def primary(self):
        kind, val = self.peek()
        if kind == "str":
            self.i += 1
            return ("str", val)
        if self.accept("("):
            e = self.ternary()
            self.take("op", ")")
            return e
        name = self.take("id")
        if self.accept("("):
            return ("call", name, self.args())
        if name != "item":
            return ("ident", name)
        keys = []
        while True:
            if self.peek() == ("op", "[") and self.peek(1)[0] == "str" and self.peek(2) == ("op", "]"):
                keys.append(self.peek(1)[1])
                self.i += 3
            elif self.peek() == ("op", ".") and self.peek(1)[0] == "id" and self.peek(2) != ("op", "("):
                keys.append(self.peek(1)[1])
                self.i += 2
            else:
                return ("path", keys)

    def args(self):
        out = []
        if self.accept(")"):
            return out
        while True:
            out.append(self.ternary())
            if self.accept(")"):
                return out
            self.take("op", ",")

    def postfix(self, e):
        while self.accept("."):
            name = self.take("id")
            e = ("method", e, name, self.args()) if self.accept("(") else ("field", e, name)
        if self.peek() == ("op", "["):
            raise self.fail()
        return e


def ottl_path(keys):
    if not keys or keys[0] not in ROOTS:
        return None
    return keys[0] + "".join(f"[{q(k)}]" for k in keys[1:])


def field_path(node, p):
    """Converts a Go `item["attributes"|"resource"][...]` field path to OTTL."""
    e = Parser(node, "field path", p).parse()
    if e[0] != "path" or len(e[1]) < 2 or e[1][0] not in ("attributes", "resource"):
        raise unsupported(node, "field path", p)
    return ottl_path(e[1])


def is_ref(e):
    return e[0] == "path" and len(e[1]) >= 2 and e[1][0] in ("attributes", "resource")


def is_env(e):
    return e[0] == "call" and e[1] == "env" and len(e[2]) == 1 and e[2][0][0] == "str"


def value_statements(node, dst, v, ignore_if_empty):
    """Returns the OTTL statements that upsert the CEL value v into dst."""
    e = Parser(node, "CEL value", v).parse()
    bad = unsupported(node, "CEL value", " ".join(v.split()))
    default, src, guard = None, None, None
    if e[0] == "str":
        if ignore_if_empty and e[1] == "":
            return []
        return [f"set({dst}, {q(e[1])})"]
    if is_env(e):
        env = f"EDXEnv({q(e[2][0][1])}, \"\")"
        return [f"set({dst}, {env})" + (f" where {env} != \"\"" if ignore_if_empty else "")]
    if e[0] == "cond" and e[1][0] == "ne" and is_env(e[1][1]) and e[1][2] == ("str", "") and e[2] == e[1][1] and e[3][0] == "str":
        env = f"EDXEnv({q(e[2][2][0][1])}, \"\")"
        default, src, guard = e[3][1], env, f"{env} != \"\""
    elif is_ref(e):
        src = ottl_path(e[1])
        default, guard = "", f"{src} != nil"
    elif e[0] == "cond" and e[1][0] == "in" and e[1][1][0] == "str" and e[1][2][0] == "path" and len(e[1][2][1]) == 1 \
            and e[1][2][1][0] in ("attributes", "resource") and e[2] == ("path", e[1][2][1] + [e[1][1][1]]) and e[3][0] == "str":
        src = ottl_path(e[2][1])
        default, guard = e[3][1], f"{src} != nil"
    elif e[0] == "field" and e[1][0] == "call" and e[1][1] == "regex_capture" and len(e[1][2]) == 2 \
            and is_ref(e[1][2][0]) and e[1][2][1][0] == "str":
        ref, re_ = ottl_path(e[1][2][0][1]), q(e[1][2][1][1])
        src = f"ExtractPatterns({ref}, {re_})[{q(e[2])}]"
        default, guard = "", f"IsString({ref}) and IsMatch({ref}, {re_})"
    else:
        raise bad
    if not ignore_if_empty:
        return [f"set({dst}, {q(default)})", f"set({dst}, {src}) where {guard}"]
    # Go skips a value that evaluates to "", so the "" default goes and the guard rejects "".
    if default != "":
        if src.startswith("EDXEnv("):
            return [f"set({dst}, {q(default)})", f"set({dst}, {src}) where {guard}"]
        raise unsupported(node, "ignore_if_empty value", " ".join(v.split()))
    if src.startswith("EDXEnv("):
        return [f"set({dst}, {src}) where {guard}"]
    return [f"set({dst}, {src}) where {guard} and {src} != \"\""]


def check_keys(n, allowed):
    for k in n:
        if k not in ("name", "type", "user_description") + allowed:
            raise unsupported(n["name"], "key", k)


def described(n, out):
    if "user_description" in n:
        out["user_description"] = n["user_description"]
    return out


def transform_node(n):
    check_keys(n, ("transformations",))
    stmts = []
    for t in n.get("transformations") or []:
        for k in t:
            if k not in ("field_path", "operation", "value", "ignore_if_empty"):
                raise unsupported(n["name"], "transformation key", k)
        dst = field_path(n["name"], t["field_path"])
        op = t.get("operation", "upsert")
        if op == "delete":
            keys = Parser(n["name"], "field path", t["field_path"]).parse()[1]
            stmts.append(f"delete_key({ottl_path(keys[:-1])}, {q(keys[-1])})")
        elif op == "upsert":
            stmts += value_statements(n["name"], dst, t["value"], t.get("ignore_if_empty", False))
        else:
            raise unsupported(n["name"], "operation", op)
    out = {k: v for k, v in n.items() if k not in ("type", "transformations")}
    out.update(type="ottl_transform", statements="\n".join(stmts))
    return out


def resource_transform(n, links):
    """Returns the route and apply nodes and the rewritten links."""
    check_keys(n, ("transformations",))
    name, checks, stmts = n["name"], [], []
    for t in n.get("transformations") or []:
        if t.get("operation", "upsert") != "upsert":
            raise unsupported(name, "operation", t.get("operation"))
        e = Parser(name, "resource value", t["value"]).parse()
        if not is_ref(e):
            raise unsupported(name, "resource value", t["value"])
        v = ottl_path(e[1])
        checks.append(f"IsString({v})")
        stmts.append(f"set(resource[{q(t['field_path'])}], {v})")
    apply = f"{name}__apply"
    route = described(n, {"name": name, "type": "route_ottl",
                          "paths": [{"path": "ok", "condition": " and ".join(checks), "exit_if_matched": True}]})
    out = []
    for l in links:
        if l["from"] != name:
            out.append(l)
        elif l.get("path", "") in ("", "success"):
            out.append({"from": apply, "to": l["to"]})
        elif l["path"] == "failure":
            out.append({"from": name, "path": "unmatched", "to": l["to"]})
        else:
            raise unsupported(name, "link path", l["path"])
    out.append({"from": name, "path": "ok", "to": apply})
    return [route, {"name": apply, "type": "ottl_transform", "statements": "\n".join(stmts)}], out


def regex_filter(n):
    check_keys(n, ("pattern", "negate"))
    if not n.get("pattern"):
        raise unsupported(n["name"], "regex_filter pattern", '""')
    return described(n, {"name": n["name"], "type": "ottl_filter",
                         "filter_mode": "exclude" if n.get("negate") else "include",
                         "condition": f"IsMatch(body, {q(n['pattern'])})"})


def capture_groups(pat):
    """Returns the start and end of every capturing group opener in pat."""
    out, i, in_class = [], 0, False
    while i < len(pat):
        c = pat[i]
        if c == "\\":
            i += 2
            continue
        if in_class:
            in_class = c != "]"
        elif c == "[":
            in_class = True
            if pat[i + 1:i + 2] == "^":
                i += 1
            if pat[i + 1:i + 2] == "]":
                i += 1
        elif c == "(":
            m = re.match(r"\(\?P?<(\w+)>", pat[i:])
            if m:
                out.append((i, i + m.end()))
            elif pat[i + 1:i + 2] != "?":
                out.append((i, i + 1))
        i += 1
    return out


def log_level_detector(n):
    check_keys(n, ("pattern", "fallback_value"))
    pat, fallback = n["pattern"], n.get("fallback_value", "")
    if fallback == "":
        raise unsupported(n["name"], "empty fallback_value", pat)
    groups = capture_groups(pat)
    if len(groups) > 1:
        raise unsupported(n["name"], "pattern with several capture groups", pat)
    # Go takes the first capture group when the pattern has one, else the whole match.
    lvl = f"{pat[:groups[0][0]]}(?P<lvl>{pat[groups[0][1]:]}" if groups else f"(?P<lvl>{pat})"
    return described(n, {"name": n["name"], "type": "ottl_transform", "statements": "\n".join([
        f"set(severity_text, {q(fallback)})",
        f"set(severity_text, ExtractPatterns(body, {q(lvl)})[\"lvl\"]) where IsString(body) and IsMatch(body, {q(pat)})"])})


def parse_json_attributes(n):
    check_keys(n, ("process_field", "field_path"))
    if n.get("process_field") != 'item["body"]':
        raise unsupported(n["name"], "process_field", n.get("process_field"))
    if n.get("field_path") != 'item["attributes"]':
        raise unsupported(n["name"], "field_path", n.get("field_path"))
    return described(n, {"name": n["name"], "type": "ottl_transform", "statements": "\n".join([
        'set(cache["json"], ParseJSON(body)) where IsString(body)',
        'set(attributes, cache["json"]) where IsMap(cache["json"]) and Len(cache["json"]) > 0'])})


def route_atom(node, src, e):
    """Returns the OTTL text, top operator and whether the atom indexes body."""
    neg = e[0] == "not"
    a = e[1] if neg else e
    fn = a[0] == "call" and a[1] in ("has", "regex_match")
    if neg and not fn:
        raise unsupported(node, "route condition", src)
    if a[0] in ("eq", "ne") and a[1][0] == "path" and a[2][0] == "str":
        keys, lit = a[1][1], q(a[2][1])
    elif fn and a[1] == "has" and len(a[2]) == 1 and a[2][0][0] == "path" and len(a[2][0][1]) >= 2:
        keys = a[2][0][1]
    elif fn and a[1] == "regex_match" and len(a[2]) == 2 and a[2][0][0] == "path" and a[2][1][0] == "str":
        keys, lit = a[2][0][1], q(a[2][1][1])
    else:
        raise unsupported(node, "route condition", src)
    p = ottl_path(keys)
    if p is None:
        raise unsupported(node, "route condition", src)
    body = keys[0] == "body" and len(keys) > 1
    # CEL errors count as no match, so each atom is false wherever CEL would error.
    if a[0] == "eq":
        return f"{p} == {lit}", "atom", body
    if a[0] == "ne":
        return f"{p} != nil and {p} != {lit}", "and", body
    if a[1] == "has":
        return f"{p} {'==' if neg else '!='} nil", "atom", body
    if neg:
        return f"IsString({p}) and not IsMatch({p}, {lit})", "and", body
    return f"IsMatch({p}, {lit})", "atom", body


def guard_body(s, op, body):
    if not body:
        return s, op
    return (f"IsMap(body) and {s}" if op in ("atom", "and") else f"IsMap(body) and ({s})"), "and"


def route_expr(node, src, e):
    """Returns the OTTL text, its top operator and whether every atom indexes body."""
    if e[0] not in ("and", "or"):
        return route_atom(node, src, e)
    parts = []
    sides = [route_expr(node, src, x) for x in e[1:]]
    all_body = all(b for _, _, b in sides)
    for s, op, b in sides:
        if not all_body:
            s, op = guard_body(s, op, b)
        parts.append(f"({s})" if e[0] == "and" and op == "or" else s)
    return f" {e[0]} ".join(parts), e[0], all_body


def route(n):
    check_keys(n, ("expression_type", "paths"))
    paths = []
    for p in n.get("paths") or []:
        for k in p:
            if k not in ("path", "condition", "exit_if_matched"):
                raise unsupported(n["name"], "route path key", k)
        src = " ".join(p["condition"].split())
        e = Parser(n["name"], "route condition", src).parse()
        s, op, body = route_expr(n["name"], src, e)
        s, _ = guard_body(s, op, body)
        paths.append({"path": p["path"], "condition": s, "exit_if_matched": p.get("exit_if_matched", False)})
    return described(n, {"name": n["name"], "type": "route_ottl", "paths": paths})


def log_to_pattern(n):
    check_keys(n, ("group_by",) + LOG_TO_PATTERN_KEYS)
    proc = {"type": "log_to_pattern_metric", "group_by": [field_path(n["name"], g) for g in n.get("group_by") or []]}
    for k in LOG_TO_PATTERN_KEYS:
        if k in n:
            proc[k] = n[k]
    return described(n, {"name": n["name"], "type": "log_to_pattern_metric", **{k: v for k, v in proc.items() if k != "type"}})


def log_to_metric(n):
    check_keys(n, ("pattern", "interval", "metric_name", "skip_empty_intervals", "only_report_nonzeros"))
    if not n.get("pattern"):
        raise unsupported(n["name"], "log_to_metric pattern", '""')
    metric = re.sub(r"[^_a-zA-Z0-9.]+", "_", n.get("metric_name") or n["name"]) + ".count"
    return described(n, {"name": n["name"], "type": "sequence", "processors": [
        {"type": "ottl_filter", "data_types": ["log"], "filter_mode": "include",
         "condition": f"IsMatch(body, {q(n['pattern'])})"},
        {"type": "extract_metric", "data_types": ["log"], "keep_item": False,
         "extract_metric_rules": [{"name": metric, "gauge": {"value": "1"}}]},
        {"type": "aggregate_metric", "data_types": ["metric"], "aggregate_metric_rules": [
            {"name": metric, "interval": n.get("interval", "1m"), "aggregation_type": "sum",
             "group_by": ['resource["__group_name"]']}]}]})


def remove_output_transform(name, types, links):
    """Splices an output_transform out of links when it only feeds ed_output."""
    outs = collections.defaultdict(list)
    for l in links:
        outs[l["from"]].append(l["to"])
    seen, todo = {name}, list(outs[name])
    while todo:
        x = todo.pop()
        if x in seen:
            continue
        seen.add(x)
        if not outs[x] and types.get(x) != "ed_output":
            raise unsupported(name, "output_transform feeding", x)
        todo += outs[x]
    targets = [l["to"] for l in links if l["from"] == name and l.get("path", "") in ("", "success")]
    if not targets:
        raise unsupported(name, "output_transform without", "links")
    out = []
    for l in links:
        if l["from"] == name:
            continue
        if l["to"] != name:
            out.append(l)
            continue
        if types.get(l["from"]) != "log_to_metric":
            raise unsupported(name, "output_transform input", l["from"])
        for t in targets:
            new = {**l, "to": t}
            if new not in out:
                out.append(new)
    return out


def strip_comments(n):
    # RED's OTTL parser has no `//` comments; Go skips those lines.
    changed = False
    if isinstance(n.get("statements"), str):
        lines = n["statements"].split("\n")
        kept = [l for l in lines if not l.strip().startswith("//")]
        changed, n["statements"] = len(kept) != len(lines), "\n".join(kept)
    for p in n.get("processors") or []:
        changed = strip_comments(p) or changed
    return changed


def lookup_paths(n):
    # RED's lookup reads OTTL paths only; Go also takes item["attributes"][...].
    changed = False
    if n.get("type") == "lookup":
        for f in (n.get("key_fields") or []) + (n.get("out_fields") or []):
            if f["event_field"].startswith("item"):
                f["event_field"], changed = field_path(n.get("name", "lookup"), f["event_field"]), True
    for p in n.get("processors") or []:
        changed = lookup_paths(p) or changed
    return changed


def translate(c):
    """Translates a parsed Go config in place and returns the summary."""
    links = list(c.get("links") or [])
    types = {n["name"]: n["type"] for n in c["nodes"]}
    translated, l2m, removed, nodes, wrap = collections.Counter(), [], [], [], set()
    simple = {"log_transform": transform_node, "generic_transform": transform_node, "regex_filter": regex_filter,
              "log_level_detector": log_level_detector, "parse_json_attributes": parse_json_attributes,
              "log_to_pattern": log_to_pattern, "log_to_metric": log_to_metric}
    for n in c["nodes"]:
        t, name = n["type"], n["name"]
        if t in simple:
            nodes.append(simple[t](n))
        elif t == "resource_transform":
            out, links = resource_transform(n, links)
            nodes += out
        elif t == "route" and n.get("expression_type", "cel") == "cel":
            nodes.append(route(n))
        elif t == "output_transform":
            links = remove_output_transform(name, types, links)
            removed.append(name)
            continue
        else:
            n = dict(n)
            changed = strip_comments(n)
            if lookup_paths(n):
                changed = True
                if t == "lookup":
                    wrap.add(name)
            if changed:
                translated[t] += 1
            nodes.append(n)
            continue
        translated[t] += 1
        if t == "log_to_metric":
            l2m.append(name)
        if t in ("log_to_pattern", "log_to_metric"):
            wrap.add(name)
    # The control plane accepts log_to_pattern_metric and OTTL lookup paths only inside a sequence.
    for i, n in enumerate(nodes):
        if n["name"] in wrap and n["type"] != "sequence":
            proc = {k: v for k, v in n.items() if k not in ("name", "user_description")}
            proc.setdefault("data_types", ["log"])
            nodes[i] = described(n, {"name": n["name"], "type": "sequence", "processors": [proc]})
    for l in links:
        if l["from"] in wrap and l.get("path") == "failure":
            if not any(x["from"] == l["from"] and x.get("path", "") == "" and x["to"] == l["to"] for x in links):
                raise unsupported(l["from"], "failure link", f"{l['to']} differs from its success links")
    c["nodes"] = nodes
    c["links"] = [l for l in links if not (l["from"] in wrap and l.get("path") == "failure")]
    return {"translated": dict(sorted(translated.items())), "removed": removed, "log_to_metric": l2m}


def main():
    if len(sys.argv) != 3:
        print(__doc__, file=sys.stderr)
        sys.exit(2)
    with open(sys.argv[1]) as f:
        c = yaml.safe_load(f)
    try:
        summary = translate(c)
    except Unsupported as e:
        print(e, file=sys.stderr)
        sys.exit(1)
    with open(sys.argv[2], "w") as f:
        yaml.safe_dump(c, f, sort_keys=False, width=1000, allow_unicode=True)
    print(json.dumps(summary, indent=2))


if __name__ == "__main__":
    main()
