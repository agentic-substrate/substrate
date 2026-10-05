#!/usr/bin/env python3
"""Measure a packaged foreground node with private synthetic data on Linux/WSL."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import signal
import socket
import statistics
import subprocess
import tempfile
import threading
import time


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", default="bin/substrate")
    parser.add_argument("--memories", type=int, default=256)
    parser.add_argument("--idle-seconds", type=float, default=2)
    args = parser.parse_args()
    binary = Path(args.binary).resolve()
    if not binary.is_file() or not os.access(binary, os.X_OK):
        parser.error("build the packaged binary first with make build")
    if args.memories < 1 or args.memories > 4096 or args.idle_seconds < 1:
        parser.error("use 1–4096 memories and at least one idle second")
    for program in ("git", "unshare"):
        if not shutil.which(program):
            parser.error(f"install {program} before measuring")
    subprocess.run(["unshare", "--user", "--map-root-user", "--net", "true"], check=True)
    hz = os.sysconf("SC_CLK_TCK")
    root = Path(tempfile.mkdtemp(prefix="sm12-", dir="/var/tmp"))
    state, repo, credential = root / "state", root / "repo", root / "session"
    repo.mkdir(mode=0o700)
    child = None
    sampling = threading.Event()
    peak_rss = [0]

    def command(action, *flags, content=None):
        result = subprocess.run([str(binary), action, "-state-dir", str(state), *flags],
                                input=content, text=True, capture_output=True, timeout=30)
        if result.returncode:
            raise RuntimeError(f"{action} failed: {result.stderr.strip()}")
        return json.loads(result.stdout)

    def cpu():
        fields = Path(f"/proc/{child.pid}/stat").read_text().split()
        return (int(fields[13]) + int(fields[14])) / hz

    def rss():
        for line in Path(f"/proc/{child.pid}/status").read_text().splitlines():
            if line.startswith("VmRSS:"):
                return int(line.split()[1])
        return 0

    def sample():
        while not sampling.wait(0.01):
            try:
                peak_rss[0] = max(peak_rss[0], rss())
            except FileNotFoundError:
                return

    def start(*flags, offline=False):
        nonlocal child
        prefix = ["unshare", "--user", "--map-root-user", "--net"] if offline else []
        child = subprocess.Popen([*prefix, str(binary), "node", "-state-dir", str(state), *flags],
                                 stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
        for _ in range(500):
            if child.poll() is not None:
                raise RuntimeError(f"node startup failed: {child.stderr.read().decode()}")
            if (state / "node.sock").exists():
                try:
                    ipc("index", control="status")
                    break
                except (ConnectionRefusedError, FileNotFoundError):
                    pass
            time.sleep(0.01)
        else:
            raise RuntimeError("node did not become available within five seconds")
        sampling.clear()
        thread = threading.Thread(target=sample)
        thread.start()
        return thread

    def stop(thread, sig):
        sampling.set()
        thread.join()
        child.send_signal(sig)
        child.wait(timeout=5)
        if child.returncode:
            raise RuntimeError(f"foreground shutdown failed: {child.stderr.read().decode()}")
        assert not (state / "node.sock").exists(), "shutdown left its socket"
        child.stderr.close()

    def ipc(action, **values):
        request = {"action": action, **values}
        if action != "index":
            request.update(token=credential.read_text().strip(), checkout=str(repo))
        with socket.socket(socket.AF_UNIX) as conn:
            conn.settimeout(30)
            conn.connect(str(state / "node.sock"))
            conn.sendall(json.dumps(request).encode() + b"\n")
            with conn.makefile("rb") as reader:
                response = json.loads(reader.readline(8 * 1024 * 1024))
        if response.get("error"):
            raise RuntimeError(f"{action} failed: {response['error']}")
        return response["result"]

    def timed(operation):
        start_time = time.perf_counter()
        result = operation()
        return result, (time.perf_counter() - start_time) * 1000

    def latency(samples):
        ordered = sorted(samples)
        return {"count": len(samples), "p50_ms": round(statistics.median(samples), 3),
                "p95_ms": round(ordered[min(len(ordered) - 1, int(len(ordered) * .95))], 3)}

    try:
        subprocess.run(["git", "-C", str(repo), "init", "-q"], check=True)
        command("init", "-owner", "Synthetic owner")
        command("register", "-path", str(repo), "-space", "Personal")
        command("session", "-path", str(repo), "-space", "Personal", "-out", str(credential))
        thread = start("-index-paused=true")
        captures, receipts = [], []
        for i in range(args.memories):
            contribution = {"operation_id": f"capture-{i}", "kind": "memory",
                            "content": f"needle{i} " + "build frontend bounded lexical evidence " * 25,
                            "provenance": "synthetic maintenance measurement"}
            receipt, elapsed = timed(lambda: ipc("capture", contribution=contribution))
            receipts.append((receipt, contribution))
            captures.append(elapsed)
        paused_cpu = cpu()
        time.sleep(args.idle_seconds)
        idle_delta = cpu() - paused_cpu
        paused = ipc("index", control="status")
        assert paused["paused"] and paused["queued"] == args.memories
        db_paused = (state / "artifacts.db").stat().st_size
        duplicate = subprocess.run([str(binary), "node", "-state-dir", str(state)],
                                   capture_output=True, text=True, timeout=5)
        assert duplicate.returncode and "already running" in duplicate.stderr
        stop(thread, signal.SIGINT)
        for name in ("SKILL.md", "reference.md", "recipe.md"):
            (repo / name).write_text("approved bundle dependency evidence " * 128)
        subprocess.run(["git", "-C", str(repo), "add", "."], check=True)
        subprocess.run(["git", "-C", str(repo), "-c", "user.name=Synthetic owner", "-c",
                        "user.email=owner@example.test", "commit", "-qm", "synthetic source"], check=True)
        commit = subprocess.check_output(["git", "-C", str(repo), "rev-parse", "HEAD"], text=True).strip()
        thread = start()
        source = command("propose", "-path", str(repo), "-credential", str(credential),
                         "-operation", "source", "-commit", commit, "-file", "SKILL.md",
                         "-dependency", "reference.md", "-dependency", "recipe.md")
        stop(thread, signal.SIGTERM)
        command("source-register", "-path", str(repo), "-artifact", source["artifact_id"],
                "-source", "repository", "-name", "measured-skill", "-alias", "measured-skill")
        command("approve", "-path", str(repo), "-artifact", source["artifact_id"],
                "-revision", source["revision_id"], "-operation", "approval")
        thread = start(offline=True)
        route_table = Path(f"/proc/{child.pid}/net/route").read_text()
        routes = [line for line in route_table.splitlines() if line.strip() and not line.startswith("Iface")]
        assert not routes, "offline namespace has a route"
        durable = ipc("index", control="status")
        assert durable["paused"] and durable["queued"] == args.memories + 1
        original, contribution = receipts[0]
        assert ipc("capture", contribution=contribution) == original, "retry identity changed"
        assert ipc("read", read={"artifact_id": original["artifact_id"]})["revision"]["id"] == original["revision_id"]
        pending_before = ipc("pending")
        incremental_cpu = cpu()
        start_time = time.perf_counter()
        ipc("index", pause=False)
        deadline = time.perf_counter() + 30
        while time.perf_counter() < deadline:
            status = ipc("index", control="status")
            if status["queued"] == 0:
                break
            time.sleep(.01)
        else:
            raise RuntimeError(f"incremental indexing did not finish within 30 seconds: {status}")
        incremental_ms = (time.perf_counter() - start_time) * 1000
        incremental_cpu_delta = cpu() - incremental_cpu
        assert not status["failed"] and not status["deferred"]
        assert ipc("pending") == pending_before, "indexing consumed acknowledged work"
        searches = []
        for i in range(40):
            found, elapsed = timed(lambda: ipc("search", search={"query": f"needle{i % args.memories}"}))
            assert len(found["results"]) == 1
            searches.append(elapsed)
        approved = ipc("read", read={"selector": "measured-skill"})
        assert len(approved["revision"]["source"]["files"]) == 2
        db_incremental = (state / "artifacts.db").stat().st_size
        bulk_cpu = cpu()
        start_time = time.perf_counter()
        queued = ipc("index", control="rebuild")
        assert queued["deferred"] == args.memories + 1
        while ipc("index", control="run")["deferred"]:
            pass
        bulk_ms = (time.perf_counter() - start_time) * 1000
        bulk_cpu_delta = cpu() - bulk_cpu
        final = ipc("browser-inventory")["maintenance"]
        assert final["state"] == "ready" and final["coverage"]["indexed"] == args.memories + 1
        ready_cpu = cpu()
        time.sleep(args.idle_seconds)
        ready_idle_delta = cpu() - ready_cpu
        result = {"kernel": os.uname().release, "machine": os.uname().machine,
                  "logical_cpus": os.cpu_count(), "cpu_model": next(line.split(":", 1)[1].strip() for line in Path("/proc/cpuinfo").read_text().splitlines() if line.startswith("model name")),
                  "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
                  "memories": args.memories, "memory_words": 126,
                  "approved_git_bundles": 1, "explicit_dependency_files": 2,
                  "capture_ipc": latency(captures), "search_ipc": latency(searches),
                  "incremental_wall_ms": round(incremental_ms, 3), "incremental_node_cpu_s": round(incremental_cpu_delta, 3),
                  "bulk_wall_ms": round(bulk_ms, 3), "bulk_node_cpu_s": round(bulk_cpu_delta, 3),
                  "idle_interval_s": args.idle_seconds, "paused_idle_node_cpu_s": round(idle_delta, 3),
                  "ready_idle_node_cpu_s": round(ready_idle_delta, 3), "sampled_peak_node_rss_kib": peak_rss[0],
                  "db_paused_bytes": db_paused, "db_indexed_bytes": db_incremental,
                  "db_rebuilt_bytes": (state / "artifacts.db").stat().st_size,
                  "offline_routes": len(routes), "offline_route_table": route_table,
                  "sigint_sigterm_duplicate_restart": "passed",
                  "receipt_current_read_search_pending_ledger": "passed", "maintenance": final}
        stop(thread, signal.SIGTERM)
        child = None
        print(json.dumps(result, indent=2))
    finally:
        sampling.set()
        if child is not None and child.poll() is None:
            child.terminate()
            child.wait(timeout=5)
        shutil.rmtree(root)


if __name__ == "__main__":
    main()
