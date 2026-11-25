"""Tests for translate.py on synthetic pipelines."""
import copy
import os
import subprocess
import sys
import tempfile
import unittest

import yaml

import translate

HERE = os.path.dirname(os.path.abspath(__file__))


def cfg(nodes, links=()):
    return {"version": "v3", "nodes": list(nodes), "links": list(links)}


def lt(*transformations, typ="log_transform"):
    return {"name": "t", "type": typ, "transformations": list(transformations)}


def up(path, value, **kw):
    return {"field_path": path, "operation": "upsert", "value": value, **kw}


def ottl(statements, name="t"):
    return {"name": name, "type": "ottl_transform", "statements": "\n".join(statements)}


def route(*paths, **kw):
    return {"name": "r", "type": "route", **kw,
            "paths": [{"path": f"p{i}", "condition": c, "exit_if_matched": True} for i, c in enumerate(paths)]}


def route_ottl(*conds):
    return {"name": "r", "type": "route_ottl",
            "paths": [{"path": f"p{i}", "condition": c, "exit_if_matched": True} for i, c in enumerate(conds)]}


OUT = {"name": "out", "type": "ed_output"}
L2M = {"name": "m", "type": "log_to_metric", "pattern": "(?i)error", "interval": "1m0s"}
L2M_SEQ = {"name": "m", "type": "sequence", "processors": [
    {"type": "ottl_filter", "data_types": ["log"], "filter_mode": "include", "condition": 'IsMatch(body, "(?i)error")'},
    {"type": "extract_metric", "data_types": ["log"], "keep_item": False,
     "extract_metric_rules": [{"name": "m.count", "gauge": {"value": "1"}}]},
    {"type": "aggregate_metric", "data_types": ["metric"], "aggregate_metric_rules": [
        {"name": "m.count", "interval": "1m0s", "aggregation_type": "sum", "group_by": ['resource["__group_name"]']}]}]}
LEVEL = 'set(severity_text, "unknown")'


class TranslateTest(unittest.TestCase):

    def test_translate(self):
        cases = [
            {
                "desc": "literal",
                "in": cfg([lt(up('item["attributes"]["a"]', '"x"'))]),
                "want": cfg([ottl(['set(attributes["a"], "x")'])]),
            },
            {
                "desc": "env with default",
                "in": cfg([lt(up('item["resource"]["a"]', 'env("K") != "" ? env("K") : "d"'))]),
                "want": cfg([ottl(['set(resource["a"], "d")',
                                   'set(resource["a"], EDXEnv("K", "")) where EDXEnv("K", "") != ""'])]),
            },
            {
                "desc": "bare env",
                "in": cfg([lt(up('item["attributes"]["a"]', 'env("K")'))]),
                "want": cfg([ottl(['set(attributes["a"], EDXEnv("K", ""))'])]),
            },
            {
                "desc": "field reference",
                "in": cfg([lt(up('item["attributes"]["a"]["b"]', 'item["resource"]["x.y"]'))]),
                "want": cfg([ottl(['set(attributes["a"]["b"], "")',
                                   'set(attributes["a"]["b"], resource["x.y"]) where resource["x.y"] != nil'])]),
            },
            {
                "desc": "in with default",
                "in": cfg([lt(up('item["attributes"]["a"]', '"k" in item["resource"] ? item["resource"]["k"] : "d"'))]),
                "want": cfg([ottl(['set(attributes["a"], "d")',
                                   'set(attributes["a"], resource["k"]) where resource["k"] != nil'])]),
            },
            {
                "desc": "regex_capture",
                "in": cfg([lt(up('item["attributes"]["f"]', 'regex_capture(item["resource"]["p"], "/(?P<f>[^/]+)$").f'))]),
                "want": cfg([ottl(['set(attributes["f"], "")',
                                   'set(attributes["f"], ExtractPatterns(resource["p"], "/(?P<f>[^/]+)$")["f"]) '
                                   'where IsString(resource["p"]) and IsMatch(resource["p"], "/(?P<f>[^/]+)$")'])]),
            },
            {
                "desc": "delete",
                "in": cfg([lt({"field_path": 'item["attributes"]["a"]["b"]', "operation": "delete"})]),
                "want": cfg([ottl(['delete_key(attributes["a"], "b")'])]),
            },
            {
                "desc": "ignore_if_empty",
                "in": cfg([lt(up('item["attributes"]["a"]', 'item["resource"]["k"]', ignore_if_empty=True),
                              up('item["attributes"]["b"]', '""', ignore_if_empty=True),
                              up('item["attributes"]["c"]', 'env("K") != "" ? env("K") : "d"', ignore_if_empty=True))]),
                "want": cfg([ottl(['set(attributes["a"], resource["k"]) where resource["k"] != nil and resource["k"] != ""',
                                   'set(attributes["c"], "d")',
                                   'set(attributes["c"], EDXEnv("K", "")) where EDXEnv("K", "") != ""'])]),
            },
            {
                "desc": "generic_transform keeps user_description",
                "in": cfg([{**lt(up('item["attributes"]["a"]', '"x"'), typ="generic_transform"), "user_description": "u"}]),
                "want": cfg([{"name": "t", "user_description": "u", "type": "ottl_transform",
                              "statements": 'set(attributes["a"], "x")'}]),
            },
            {
                "desc": "resource_transform links",
                "in": cfg([{"name": "rt", "type": "resource_transform", "transformations": [
                    {"field_path": "a", "value": 'item["resource"]["x"]'},
                    {"field_path": "b", "value": 'item["attributes"]["y"]'}]}, OUT],
                    [{"from": "in", "to": "rt"}, {"from": "rt", "to": "out"},
                     {"from": "rt", "path": "failure", "to": "other"}]),
                "want": cfg([{"name": "rt", "type": "route_ottl", "paths": [
                    {"path": "ok", "condition": 'IsString(resource["x"]) and IsString(attributes["y"])', "exit_if_matched": True}]},
                    ottl(['set(resource["a"], resource["x"])', 'set(resource["b"], attributes["y"])'], name="rt__apply"), OUT],
                    [{"from": "in", "to": "rt"}, {"from": "rt__apply", "to": "out"},
                     {"from": "rt", "path": "unmatched", "to": "other"}, {"from": "rt", "path": "ok", "to": "rt__apply"}]),
            },
            {
                "desc": "regex_filter",
                "in": cfg([{"name": "f", "type": "regex_filter", "pattern": "a\\d"},
                           {"name": "g", "type": "regex_filter", "pattern": "b", "negate": True}]),
                "want": cfg([{"name": "f", "type": "ottl_filter", "filter_mode": "include", "condition": 'IsMatch(body, "a\\\\d")'},
                             {"name": "g", "type": "ottl_filter", "filter_mode": "exclude", "condition": 'IsMatch(body, "b")'}]),
            },
            {
                "desc": "log_level_detector",
                "in": cfg([{"name": "a", "type": "log_level_detector", "pattern": "INFO|WARN(?:ING)?", "fallback_value": "unknown"},
                           {"name": "b", "type": "log_level_detector", "pattern": "level=(\\w+)", "fallback_value": "unknown"},
                           {"name": "c", "type": "log_level_detector", "pattern": "[(]lvl=(?P<x>\\w+)", "fallback_value": "unknown"}]),
                "want": cfg([ottl([LEVEL, 'set(severity_text, ExtractPatterns(body, "(?P<lvl>INFO|WARN(?:ING)?)")["lvl"]) '
                                          'where IsString(body) and IsMatch(body, "INFO|WARN(?:ING)?")'], name="a"),
                             ottl([LEVEL, 'set(severity_text, ExtractPatterns(body, "level=(?P<lvl>\\\\w+)")["lvl"]) '
                                          'where IsString(body) and IsMatch(body, "level=(\\\\w+)")'], name="b"),
                             ottl([LEVEL, 'set(severity_text, ExtractPatterns(body, "[(]lvl=(?P<lvl>\\\\w+)")["lvl"]) '
                                          'where IsString(body) and IsMatch(body, "[(]lvl=(?P<x>\\\\w+)")'], name="c")]),
            },
            {
                "desc": "parse_json_attributes",
                "in": cfg([{"name": "j", "type": "parse_json_attributes", "process_field": 'item["body"]',
                            "field_path": 'item["attributes"]'}]),
                "want": cfg([ottl(['set(cache["json"], ParseJSON(body)) where IsString(body)',
                                   'set(attributes, cache["json"]) where IsMap(cache["json"]) and Len(cache["json"]) > 0'], name="j")]),
            },
            {
                "desc": "route atoms",
                "in": cfg([route('item["attributes"]["a"] == "x"', 'item["resource"]["a"] != "x"',
                                 'has(item.attributes.a) && !has(item["resource"]["b"])',
                                 'regex_match(item["attributes"]["a"], "^x") || !regex_match(item["attributes"]["b"], "y")',
                                 expression_type="cel")]),
                "want": cfg([route_ottl('attributes["a"] == "x"', 'resource["a"] != nil and resource["a"] != "x"',
                                        'attributes["a"] != nil and resource["b"] == nil',
                                        'IsMatch(attributes["a"], "^x") or IsString(attributes["b"]) and not IsMatch(attributes["b"], "y")')]),
            },
            {
                "desc": "route body guard",
                "in": cfg([route('(item["body"]["a"] == "1" || item["body"]["a"] == "2") && (item["body"]["b"] == "x" || !has(item.body.b))',
                                 'item["attributes"]["s"] == "x" || item["body"]["a"] == "1"')]),
                "want": cfg([route_ottl('IsMap(body) and (body["a"] == "1" or body["a"] == "2") and (body["b"] == "x" or body["b"] == nil)',
                                        'attributes["s"] == "x" or IsMap(body) and body["a"] == "1"')]),
            },
            {
                "desc": "OTTL route unchanged",
                "in": cfg([route('attributes["a"] == "x"', expression_type="ottl")]),
                "want": cfg([route('attributes["a"] == "x"', expression_type="ottl")]),
            },
            {
                "desc": "log_to_pattern",
                "in": cfg([{"name": "p", "type": "log_to_pattern", "group_by": ['item["attributes"]["a"]'], "num_of_clusters": 10}],
                          [{"from": "p", "to": "out"}, {"from": "p", "path": "failure", "to": "out"}]),
                "want": cfg([{"name": "p", "type": "sequence", "processors": [
                    {"type": "log_to_pattern_metric", "group_by": ['attributes["a"]'], "num_of_clusters": 10, "data_types": ["log"]}]}],
                    [{"from": "p", "to": "out"}]),
            },
            {
                "desc": "log_to_metric and output_transform",
                "in": cfg([L2M, {"name": "ot", "type": "output_transform", "transformations": []},
                           {"name": "s", "type": "sequence"}, OUT],
                          [{"from": "in", "to": "m"}, {"from": "m", "to": "ot"}, {"from": "ot", "to": "s"}, {"from": "s", "to": "out"}]),
                "want": cfg([L2M_SEQ, {"name": "s", "type": "sequence"}, OUT],
                            [{"from": "in", "to": "m"}, {"from": "m", "to": "s"}, {"from": "s", "to": "out"}]),
            },
            {
                "desc": "log_to_metric name and default interval",
                "in": cfg([{"name": "m", "type": "log_to_metric", "pattern": "(?i)error", "metric_name": "a-b c"}]),
                "want": cfg([{**L2M_SEQ, "processors": [L2M_SEQ["processors"][0],
                    {**L2M_SEQ["processors"][1], "extract_metric_rules": [{"name": "a_b_c.count", "gauge": {"value": "1"}}]},
                    {**L2M_SEQ["processors"][2], "aggregate_metric_rules": [
                        {"name": "a_b_c.count", "interval": "1m", "aggregation_type": "sum", "group_by": ['resource["__group_name"]']}]}]}]),
            },
            {
                "desc": "comments and lookup",
                "in": cfg([{"name": "s", "type": "sequence", "processors": [
                    {"type": "ottl_transform", "statements": "// note\nset(attributes[\"a\"], \"b\")"}]},
                    {"name": "k", "type": "lookup", "location_path": "file:///t.csv",
                     "key_fields": [{"event_field": 'item["attributes"]["a"]', "lookup_field": "a"}],
                     "out_fields": [{"event_field": 'item["attributes"]["b"]', "lookup_field": "b"}]}]),
                "want": cfg([{"name": "s", "type": "sequence", "processors": [
                    {"type": "ottl_transform", "statements": "set(attributes[\"a\"], \"b\")"}]},
                    {"name": "k", "type": "sequence", "processors": [
                        {"type": "lookup", "location_path": "file:///t.csv",
                         "key_fields": [{"event_field": 'attributes["a"]', "lookup_field": "a"}],
                         "out_fields": [{"event_field": 'attributes["b"]', "lookup_field": "b"}], "data_types": ["log"]}]}]),
            },
        ]
        for tc in cases:
            with self.subTest(tc["desc"]):
                got = copy.deepcopy(tc["in"])
                translate.translate(got)
                if got != tc["want"]:
                    self.fail(f"{tc['desc']}: got: {got}, want: {tc['want']}")

    def test_summary(self):
        c = cfg([lt(up('item["attributes"]["a"]', '"x"')), L2M, {"name": "ot", "type": "output_transform"}, OUT],
                [{"from": "m", "to": "ot"}, {"from": "ot", "to": "out"}])
        got = translate.translate(c)
        want = {"translated": {"log_to_metric": 1, "log_transform": 1}, "removed": ["ot"], "log_to_metric": ["m"]}
        if got != want:
            self.fail(f"summary: got: {got}, want: {want}")

    def test_rejected(self):
        cases = [
            ("convert_timestamp", cfg([lt(up('item["attributes"]["t"]', 'convert_timestamp(item["attributes"]["t"], "a", "b")'))]),
             't: unsupported CEL value: convert_timestamp(item["attributes"]["t"], "a", "b")'),
            ("CEL macro", cfg([lt(up('item["attributes"]["a"]', 'item["attributes"]["l"].exists(x, x == "a")'))]),
             't: unsupported CEL value: item["attributes"]["l"].exists(x, x == "a")'),
            ("ec2_metadata", cfg([lt(up('item["attributes"]["a"]', 'ec2_metadata("region")'))]),
             't: unsupported CEL value: ec2_metadata("region")'),
            ("from_k8s", cfg([lt(up('item["attributes"]["a"]', 'from_k8s(item["resource"]["p"], "x")'))]),
             't: unsupported CEL value: from_k8s(item["resource"]["p"], "x")'),
            ("body field path", cfg([lt(up('item["body"]', '"x"'))]),
             't: unsupported field path: item["body"]'),
            ("body value", cfg([lt(up('item["attributes"]["a"]', 'item["body"]["k"]'))]),
             't: unsupported CEL value: item["body"]["k"]'),
            ("operation", cfg([lt({"field_path": 'item["attributes"]["a"]', "operation": "append", "value": '"x"'})]),
             "t: unsupported operation: append"),
            ("ignore_if_empty in with default", cfg([lt(up('item["attributes"]["a"]', '"k" in item["resource"] ? item["resource"]["k"] : "d"',
                                                           ignore_if_empty=True))]),
             't: unsupported ignore_if_empty value: "k" in item["resource"] ? item["resource"]["k"] : "d"'),
            ("resource_transform literal", cfg([{"name": "rt", "type": "resource_transform",
                                                 "transformations": [{"field_path": "a", "value": '"x"'}]}]),
             'rt: unsupported resource value: "x"'),
            ("regex_filter field_path", cfg([{"name": "f", "type": "regex_filter", "pattern": "a", "field_path": 'item["attributes"]["a"]'}]),
             "f: unsupported key: field_path"),
            ("regex_filter pattern_field_path", cfg([{"name": "f", "type": "regex_filter", "pattern_field_path": 'item["attributes"]["a"]'}]),
             "f: unsupported key: pattern_field_path"),
            ("empty fallback", cfg([{"name": "d", "type": "log_level_detector", "pattern": "INFO"}]),
             "d: unsupported empty fallback_value: INFO"),
            ("several groups", cfg([{"name": "d", "type": "log_level_detector", "pattern": "(a)(b)", "fallback_value": "x"}]),
             "d: unsupported pattern with several capture groups: (a)(b)"),
            ("parse_json_attributes field_path", cfg([{"name": "j", "type": "parse_json_attributes", "process_field": 'item["body"]',
                                                       "field_path": 'item["attributes"]["j"]'}]),
             'j: unsupported field_path: item["attributes"]["j"]'),
            ("route negation", cfg([route('!(item["attributes"]["a"] == "x")')]),
             'r: unsupported route condition: !(item["attributes"]["a"] == "x")'),
            ("route _type", cfg([route('item["_type"] == "log"')]),
             'r: unsupported route condition: item["_type"] == "log"'),
            ("route function", cfg([route('size(item["attributes"]["a"]) == "1"')]),
             'r: unsupported route condition: size(item["attributes"]["a"]) == "1"'),
            ("route single quotes", cfg([route("item['attributes']['a'] == 'x'")]),
             "r: unsupported route condition: item['attributes']['a'] == 'x'"),
            ("dimension_groups", cfg([{**L2M, "dimension_groups": [{"field_dimensions": ['item["attributes"]["a"]']}]}]),
             "m: unsupported key: dimension_groups"),
            ("output_transform to other output", cfg([L2M, {"name": "ot", "type": "output_transform"}, {"name": "h", "type": "http_output"}],
                                                     [{"from": "m", "to": "ot"}, {"from": "ot", "to": "h"}]),
             "ot: unsupported output_transform feeding: h"),
            ("output_transform fed by logs", cfg([{"name": "i", "type": "http_input"}, {"name": "ot", "type": "output_transform"}, OUT],
                                                 [{"from": "i", "to": "ot"}, {"from": "ot", "to": "out"}]),
             "ot: unsupported output_transform input: i"),
            ("wrapped failure link", cfg([{"name": "p", "type": "log_to_pattern"}],
                                         [{"from": "p", "to": "out"}, {"from": "p", "path": "failure", "to": "other"}]),
             "p: unsupported failure link: other differs from its success links"),
        ]
        for desc, c, want in cases:
            with self.subTest(desc):
                try:
                    translate.translate(c)
                    got = None
                except translate.Unsupported as e:
                    got = str(e)
                if got != want:
                    self.fail(f"{desc}: got: {got}, want: {want}")

    def test_cli(self):
        cases = [
            {"desc": "translated", "in": cfg([lt(up('item["attributes"]["a"]', '"x"'))]), "code": 0,
             "want": cfg([ottl(['set(attributes["a"], "x")'])])},
            {"desc": "rejected writes nothing", "in": cfg([lt(up('item["attributes"]["a"]', 'convert_timestamp("x")'))]), "code": 1,
             "want": None},
        ]
        for tc in cases:
            with self.subTest(tc["desc"]), tempfile.TemporaryDirectory() as d:
                src, dst = os.path.join(d, "go.yaml"), os.path.join(d, "red.yaml")
                with open(src, "w") as f:
                    yaml.safe_dump(tc["in"], f)
                p = subprocess.run([sys.executable, os.path.join(HERE, "translate.py"), src, dst], capture_output=True, text=True)
                got = None
                if os.path.exists(dst):
                    with open(dst) as f:
                        got = yaml.safe_load(f)
                if (p.returncode, got) != (tc["code"], tc["want"]):
                    self.fail(f"{tc['desc']}: got: {(p.returncode, got, p.stderr)}, want: {(tc['code'], tc['want'])}")


if __name__ == "__main__":
    unittest.main()
