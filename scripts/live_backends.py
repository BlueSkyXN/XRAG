#!/usr/bin/env python3
"""Opt-in, non-mocked XRAG/Qdrant/Neo4j/model integration test.

Does not install services or modify an existing XRAG data directory. It writes a
new randomly named test KB into the configured backends and may incur model fees.
External test data is retained for inspection; the report identifies its origin.
"""
from __future__ import annotations
import argparse
import base64
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[1]


def resolve_collection(config: dict, reference: dict) -> str:
    """按测试引用的资料域、KB 和模型身份读取 Qdrant descriptor。"""
    identity = {"origin_id": reference["o"], "kb": reference["k"], "model_identity": reference["g"]}
    if any(not isinstance(value, str) or not value for value in identity.values()):
        raise ValueError("Semantic reference is missing its origin, KB or model identity")
    headers = {"Content-Type": "application/json"}
    key_ref = config["qdrant"].get("key", "")
    if key_ref:
        if key_ref.startswith("env:"):
            key = os.environ.get(key_ref[4:], "").strip()
        elif key_ref.startswith("file:"):
            key = Path(key_ref[5:]).read_text().strip()
        else:
            raise ValueError("Qdrant credentials require an env: or file: reference")
        if not key:
            raise ValueError("Qdrant credential reference is empty or unavailable")
        headers["api-key"] = key
    body = {"filter": {"must": [{"key": key, "match": {"value": value}} for key, value in identity.items()]},
            "limit": 2, "with_payload": True, "with_vector": False}
    request = urllib.request.Request(config["qdrant"]["url"].rstrip("/") + "/collections/xrag_demo_meta/points/scroll",
                                     data=json.dumps(body).encode(), headers=headers)
    with urllib.request.urlopen(request, timeout=30) as response:
        result = json.load(response)["result"]
    points = result.get("points", [])
    # 本脚本创建随机 KB，当前模型应有且仅有一个 descriptor；不能任取同名模型的集合。
    if len(points) != 1 or result.get("next_page_offset") is not None:
        raise ValueError("Expected exactly one Qdrant descriptor for the test origin, KB and model")
    descriptor = points[0].get("payload", {})
    expected = {**identity, "model": config["embedding"]["model"], "dimension": config["embedding"]["dimension"]}
    if any(descriptor.get(key) != value for key, value in expected.items()):
        raise ValueError("Qdrant descriptor does not match the test reference and embedding configuration")
    collection = descriptor.get("physical_collection")
    if not isinstance(collection, str) or not collection.strip():
        raise ValueError("Qdrant descriptor is missing physical_collection")
    return collection


def run(config_path: Path, report_path: Path, confirmed: bool) -> int:
    report = {"status": "not_run", "scope": "configured real HTTP services; no fixture server is started", "checks": [], "external_data_retained": True}
    proc = None
    try:
        if not confirmed:
            raise ValueError("Pass --confirm-services: this test writes a new test namespace and may incur embedding fees")
        cfg = json.loads(config_path.read_text())
        for name in ("embedding", "qdrant", "neo4j"):
            if not isinstance(cfg.get(name), dict) or not cfg[name].get("url"):
                raise ValueError(f"Configuration requires {name}.url")
        # Avoid invoking unrequested optional services or local extractors.
        cfg = {k: cfg[k] for k in ("embedding", "qdrant", "neo4j")}
        server, client = ROOT / "bin/xrag-server", ROOT / "bin/xrag"
        if not server.exists() or not client.exists():
            raise ValueError("Run make build first")

        def check(name, condition, **details):
            report["checks"].append({"name": name, "passed": bool(condition), **details})
            if not condition:
                raise AssertionError(name)

        def command(argv, **kwargs):
            return subprocess.run([str(x) for x in argv], check=True, capture_output=True,
                                  text=True, timeout=180, **kwargs).stdout

        with tempfile.TemporaryDirectory(prefix="xrag-live-backends-") as tmp:
            root = Path(tmp)
            data, docs = root / "data", root / "corpus"
            docs.mkdir()
            (docs / "relations.md").write_text(
                "# XRAG integration fixture / 合成关系\n\n"
                "E203 表示日志采集失败。\n\n"
                "[[设备A]] --使用--> [[组件B]]\n"
                "[[组件B]] --记录--> [[日志事件]]\n", encoding="utf-8")
            (docs / "unrelated.md").write_text("# unrelated\n设备C 的安装说明与日志事件无关。\n", encoding="utf-8")
            conf = root / "providers.json"
            conf.write_text(json.dumps(cfg))
            os.chmod(conf, 0o600)
            command([server, "init", "--data", data])
            token = (data / "admin.key").read_text().strip()
            with socket.socket() as sock:
                sock.bind(("127.0.0.1", 0)); port = sock.getsockname()[1]
            base = f"http://127.0.0.1:{port}"
            with (root / "server.log").open("w") as logfile:
                proc = subprocess.Popen([str(server), "serve", "--data", str(data),
                    "--listen", f"127.0.0.1:{port}", "--config", str(conf)],
                    stdout=logfile, stderr=logfile)
                for _ in range(100):
                    if proc.poll() is not None:
                        raise RuntimeError("XRAG exited before readiness; check model and credential configuration")
                    try:
                        urllib.request.urlopen(base + "/healthz", timeout=1).close(); break
                    except (OSError, urllib.error.URLError):
                        time.sleep(.1)
                else:
                    raise RuntimeError("XRAG readiness timeout")

                def api(path, body=None):
                    request = urllib.request.Request(base + path,
                        data=None if body is None else json.dumps(body).encode(),
                        headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"})
                    with urllib.request.urlopen(request, timeout=180) as response:
                        return json.load(response)

                kb = "live_" + uuid.uuid4().hex[:10]
                report["kb"] = kb
                api("/v1/kbs", {"id": kb, "name": "Temporary integration fixture"})
                api(f"/v1/kbs/{kb}/sources", {"id": "fixture", "kind": "path", "root": str(docs), "mount": "fixture"})
                job = api(f"/v1/kbs/{kb}/builds", {"mode": "incremental"})
                expires = time.monotonic() + 600
                while time.monotonic() < expires:
                    result = api("/v1/jobs/" + job["id"])
                    if result["state"] not in ("queued", "running"):
                        break
                    time.sleep(.4)
                else:
                    raise RuntimeError("build timeout")
                check("real configured backends complete build", result["state"] == "succeeded", state=result["state"])
                lexical = api("/v1/search/keyword", {"kbs": [kb], "q": "E203"})
                check("Server lexical query after professional backend build", lexical["returned"] > 0)
                semantic = api("/v1/search/semantic", {"kbs": [kb], "q": "日志采集失败"})
                check("Server semantic query invokes configured Embedding and Qdrant", semantic["returned"] > 0)
                raw_ref = semantic["items"][0]["ref"]
                encoded = raw_ref.split(":", 1)[1]
                ref = json.loads(base64.urlsafe_b64decode(encoded + "=" * (-len(encoded) % 4)))
                origin = ref["o"]
                report["origin"] = origin
                check("semantic reference belongs to the test KB", ref["k"] == kb)
                collection = resolve_collection(cfg, ref)
                report.update({"origin": origin, "physical_collection": collection,
                    "cleanup_note": "Only remove this test collection and graph nodes matching this exact origin after inspection. Never delete the shared xrag_demo_meta collection."})
                read = api("/v1/files/read", {"kbs": [kb], "ref": raw_ref})
                check("Server reference returns actual stored text", bool(read["data"].get("text")))
                graph = api("/v1/graph/path", {"kbs": [kb], "from_id": "设备A", "to_id": "日志事件", "direction": "out"})
                check("Neo4j Query API returns a two-edge path", graph["returned"] > 0)
                ask = api("/v1/ask", {"kbs": [kb], "q": "组件B", "graph": True, "rerank": False, "trace": True})
                check("joint fixed retrieval completes without hidden partial failure", ask["returned"] > 0 and not ask["coverage"]["execution_partial"])

                p = root / "direct.json"
                p.write_text(json.dumps({"default_profile": "live", "profiles": {"live": {
                    "profile_id": "p_live", "type": "direct", "default_kbs": [kb], "origin_id": origin,
                    "physical_collection": collection, **cfg}}}))
                os.chmod(p, 0o600)
                proc.terminate(); proc.wait(timeout=20); proc = None
                direct = json.loads(command([client, "search", "semantic", "日志采集失败", "--config", p, "--profile", "live"]))
                check("Client direct semantic works with XRAG Server stopped", direct["returned"] > 0 and direct["mode"] == "direct")
                dref = direct["items"][0]["ref"]
                dread = json.loads(command([client, "files", "read", "--ref", dref, "--config", p, "--profile", "live"]))
                check("Client direct reads Qdrant payload by original reference", bool(dread["data"].get("text")))
                dgraph = json.loads(command([client, "graph", "path", "--from", "设备A", "--to", "日志事件", "--direction", "out", "--config", p, "--profile", "live"]))
                check("Client direct graph works with XRAG Server stopped", dgraph["returned"] > 0)
                report["status"] = "passed"
                report["interpretation"] = "Connectivity/protocol/data-flow checks only; this small synthetic corpus does not measure retrieval quality or production capacity."
    except Exception as exc:
        report["status"] = "failed" if confirmed else "not_run"
        # No provider response bodies, headers, configuration or token values in report.
        report["error"] = {"type": type(exc).__name__, "message": str(exc)[:500]}
    finally:
        if proc is not None:
            proc.terminate()
            try: proc.wait(timeout=15)
            except subprocess.TimeoutExpired: proc.kill(); proc.wait()
        report["passed"] = sum(c["passed"] for c in report["checks"])
        report_path.parent.mkdir(parents=True, exist_ok=True)
        report_path.write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
        print(json.dumps({"status": report["status"], "passed": report["passed"], "report": str(report_path)}, ensure_ascii=False))
    return 0 if report["status"] == "passed" else 2


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, required=True)
    parser.add_argument("--report", type=Path, required=True, help="Output JSON report path")
    parser.add_argument("--confirm-services", action="store_true", help="Permit external test writes and possible paid model calls")
    args = parser.parse_args()
    sys.exit(run(args.config, args.report, args.confirm_services))
