#!/usr/bin/env python3
"""Verify packaged local recovery with private synthetic state and zero network routes."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import signal
import socket
import sqlite3
import subprocess
import sys
import tempfile
import time


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", default="bin/substrate")
    parser.add_argument("--isolated", action="store_true", help=argparse.SUPPRESS)
    args = parser.parse_args()
    binary = Path(args.binary).resolve()
    if not binary.is_file() or not os.access(binary, os.X_OK):
        parser.error("build the packaged executable first with make build")
    for program in ("git", "unshare"):
        if not shutil.which(program):
            parser.error(f"install {program} before verifying recovery")
    if not args.isolated:
        subprocess.run(["unshare", "--user", "--map-root-user", "--net", sys.executable,
                        str(Path(__file__).resolve()), "--binary", str(binary), "--isolated"], check=True)
        return
    root = Path(tempfile.mkdtemp(prefix="sr11-", dir="/var/tmp"))
    state, backup, restored = root / "state", root / "backup", root / "restored"
    personal, work = root / "personal", root / "work"
    old_session, work_session, fresh_session = root / "old-session", root / "work-session", root / "fresh-session"
    consumed_grant, active_grant = root / "consumed-grant", root / "active-grant"
    child = None

    def command(action, *flags, directory=None):
        result = subprocess.run([str(binary), action, "-state-dir", str(directory or state), *flags],
                                capture_output=True, text=True, timeout=30)
        if result.returncode:
            raise RuntimeError(f"{action} failed: {result.stderr.strip()}")
        return json.loads(result.stdout)

    def ipc(action, *, credential=None, checkout=None, denied=False, **values):
        request = {"action": action, **values}
        if credential:
            request.update(token=credential.read_text().strip(), checkout=str(checkout))
        with socket.socket(socket.AF_UNIX) as conn:
            conn.settimeout(30)
            conn.connect(str(state / "node.sock"))
            conn.sendall(json.dumps(request).encode() + b"\n")
            with conn.makefile("rb") as reader:
                response = json.loads(reader.readline(8 * 1024 * 1024))
        if denied:
            assert response.get("code") == "denied" and "result" not in response, response
            return
        if response.get("error"):
            raise RuntimeError(f"{action} failed: {response['error']}")
        return response["result"]

    def start(*flags):
        nonlocal child
        child = subprocess.Popen([str(binary), "node", "-state-dir", str(state), *flags],
                                 stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
        for _ in range(500):
            if child.poll() is not None:
                raise RuntimeError(f"node startup failed: {child.stderr.read().decode()}")
            try:
                return ipc("index", control="status")
            except (FileNotFoundError, ConnectionRefusedError):
                time.sleep(.01)
        raise RuntimeError("node did not start within five seconds")

    def stop():
        nonlocal child
        child.send_signal(signal.SIGTERM)
        child.wait(timeout=5)
        assert child.returncode == 0 and not (state / "node.sock").exists()
        child.stderr.close()
        child = None

    try:
        for repo in (personal, work):
            repo.mkdir(mode=0o700)
            subprocess.run(["git", "-C", str(repo), "init", "-q"], check=True)
        command("init", "-owner", "Synthetic owner")
        destination = command("register", "-path", str(personal), "-space", "Personal")
        command("space", "-name", "Work")
        command("register", "-path", str(work), "-space", "Work")
        command("session", "-path", str(personal), "-space", "Personal", "-out", str(old_session))
        command("session", "-path", str(work), "-space", "Work", "-out", str(work_session))
        start("-index-paused=true")
        ipc("capture", credential=old_session, checkout=personal,
            contribution={"operation_id": "baseline", "kind": "memory", "content": "baseline observation"})
        source = ipc("capture", credential=work_session, checkout=work,
                     contribution={"operation_id": "work", "kind": "memory", "content": "private Work source"})
        stop()
        command("publication-policy", "-path", str(work), "-action", "export", "-decision", "allow")
        command("publication-policy", "-path", str(personal), "-action", "publish", "-decision", "allow")
        start()
        publication = {"operation_id": "proposal", "content": "reviewed separate lesson",
                       "sources": [{"artifact_id": source["artifact_id"], "revision_id": source["revision_id"]}],
                       "destination": {"space_id": destination["space_id"], "repo_id": destination["repo_id"]}}
        proposal = ipc("publication-propose", credential=work_session, checkout=work, publication=publication)
        stop()
        command("review-grant", "-path", str(work), "-proposal", proposal["id"], "-revision", proposal["revision_id"],
                "-destination-path", str(personal), "-out", str(consumed_grant))
        start()
        review = ipc("publication-review", token=consumed_grant.read_text().strip())
        approval = {"operation_id": "publish", "revision_id": proposal["revision_id"], "snapshot": review["snapshot"]}
        ipc("publication-publish", token=consumed_grant.read_text().strip(), publication_approval=approval)
        publication.update(operation_id="draft", content="unapproved lesson")
        draft = ipc("publication-propose", credential=work_session, checkout=work, publication=publication)
        ipc("index", control="rebuild")
        contribution = {"operation_id": "pending", "kind": "memory", "content": "recoverneedle captured while paused"}
        receipt = ipc("capture", credential=old_session, checkout=personal, contribution=contribution)
        pending = ipc("pending", credential=old_session, checkout=personal)
        maintenance = ipc("index", control="status")
        assert maintenance["paused"] and maintenance["queued"] == 1 and maintenance["deferred"] == 3
        stop()
        command("review-grant", "-path", str(work), "-proposal", draft["id"], "-revision", draft["revision_id"],
                "-destination-path", str(personal), "-out", str(active_grant))
        command("backup", "-out", str(backup))
        assert sorted(path.name for path in backup.iterdir()) == ["artifacts.db", "authority.json", "manifest.json"]
        assert backup.stat().st_mode & 0o777 == 0o700
        assert all(path.stat().st_mode & 0o777 == 0o600 for path in backup.iterdir())
        start()
        ipc("capture", credential=old_session, checkout=personal,
            contribution={"operation_id": "after", "kind": "memory", "content": "aftersnapshotneedle excluded"})
        stop()
        command("restore", "-backup", str(backup), directory=restored)
        state = restored
        command("session", "-path", str(personal), "-space", "Personal", "-out", str(fresh_session))
        restarted = start()
        route_table = Path(f"/proc/{child.pid}/net/route").read_text().splitlines()
        routes = [line for line in route_table if line.strip() and not line.startswith("Iface")]
        assert not routes and restarted == maintenance
        namespace = os.readlink(f"/proc/{child.pid}/ns/net")
        ipc("read", credential=old_session, checkout=personal, read={"artifact_id": receipt["artifact_id"]}, denied=True)
        ipc("publication-review", token=active_grant.read_text().strip(), denied=True)
        ipc("publication-publish", token=consumed_grant.read_text().strip(), publication_approval=approval, denied=True)
        assert ipc("pending", credential=fresh_session, checkout=personal) == pending
        assert ipc("capture", credential=fresh_session, checkout=personal, contribution=contribution) == receipt
        assert ipc("read", credential=fresh_session, checkout=personal,
                   read={"artifact_id": receipt["artifact_id"]})["revision"]["id"] == receipt["revision_id"]
        offline_contribution = {"operation_id": "new-offline", "kind": "memory", "content": "offlineneedle newly captured without routes"}
        offline_receipt = ipc("capture", credential=fresh_session, checkout=personal, contribution=offline_contribution)
        pending = ipc("pending", credential=fresh_session, checkout=personal)
        stop()
        second_restart = start()
        assert second_restart["paused"] and second_restart["queued"] == 2
        assert os.readlink(f"/proc/{child.pid}/ns/net") == namespace
        assert not [line for line in Path(f"/proc/{child.pid}/net/route").read_text().splitlines()
                    if line.strip() and not line.startswith("Iface")]
        assert ipc("capture", credential=fresh_session, checkout=personal, contribution=offline_contribution) == offline_receipt
        assert ipc("read", credential=fresh_session, checkout=personal,
                   read={"artifact_id": offline_receipt["artifact_id"]})["revision"]["id"] == offline_receipt["revision_id"]
        assert ipc("pending", credential=fresh_session, checkout=personal) == pending
        resumed = ipc("index", pause=False)
        assert resumed["processed"] == 2 and resumed["deferred"] == 3
        completed = ipc("index", control="run")
        assert completed["state"] == "ready" and completed["processed"] == 3
        found = ipc("search", credential=fresh_session, checkout=personal, search={"query": "recoverneedle"})
        assert len(found["results"]) == 1 and found["results"][0]["revision_id"] == receipt["revision_id"]
        offline_found = ipc("search", credential=fresh_session, checkout=personal, search={"query": "offlineneedle"})
        assert len(offline_found["results"]) == 1 and offline_found["results"][0]["revision_id"] == offline_receipt["revision_id"]
        assert not ipc("search", credential=fresh_session, checkout=personal,
                       search={"query": "aftersnapshotneedle"})["results"]
        assert not ipc("search", credential=fresh_session, checkout=personal,
                       search={"query": "private Work source"})["results"]
        assert ipc("pending", credential=fresh_session, checkout=personal) == pending
        with sqlite3.connect(f"file:{state}/artifacts.db?mode=ro", uri=True) as db:
            assert db.execute("SELECT count(*) FROM review_grants").fetchone()[0] == 0
            assert db.execute("SELECT count(*) FROM publication_proposals WHERE receipt<>'' AND review<>''").fetchone()[0] == 1
        print(json.dumps({"kernel": os.uname().release, "machine": os.uname().machine,
                          "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(), "schema": 5,
                          "offline_routes": 0, "restored_paused": restarted["paused"],
                          "new_offline_capture_second_restart": "passed", "same_network_namespace": True,
                          "pending_operations": len(pending), "resumed_incremental": resumed["processed"],
                          "completed_bulk": completed["processed"], "scoped_search": found["index"],
                          "old_sessions_active_consumed_reviews": "denied", "private_publication_audit": "preserved",
                          "post_snapshot_content": "absent", "receipt_pending_read_search": "passed"}, indent=2))
        stop()
    finally:
        if child is not None:
            child.kill()
            child.wait(timeout=5)
            child.stderr.close()
        shutil.rmtree(root)


if __name__ == "__main__":
    main()
