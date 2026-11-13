"""Exercise the real workspace HTTP API against an explicitly disposable fixture.

Never point this at production sources. --fixture-root must contain the marker
.filebrowser-disposable and Music/Inbox directories mounted by the trial app.
Credentials are read from a private JSON file and never printed.
"""
import argparse
import hashlib
import http.cookiejar
import json
from pathlib import Path
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", required=True)
    parser.add_argument("--credentials", type=Path, required=True)
    parser.add_argument("--fixture-root", type=Path, required=True)
    args = parser.parse_args()
    root = args.fixture_root.resolve()
    if not (root / ".filebrowser-disposable").is_file():
        parser.error("the fixture root must carry .filebrowser-disposable")
    secret = json.loads(args.credentials.read_text())
    client = urllib.request.build_opener(
        urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar())
    )
    base = args.base_url.rstrip("/")

    def request(path, body=None, method=None):
        req = urllib.request.Request(
            base + path,
            data=None if body is None else json.dumps(body).encode(),
            headers={"Content-Type": "application/json"},
            method=method,
        )
        try:
            with client.open(req, timeout=20) as response:
                return json.load(response)
        except urllib.error.HTTPError as error:
            detail = error.read(4096).decode(errors="replace")
            raise urllib.error.HTTPError(error.url, error.code, detail, error.headers, None) from None

    login = urllib.request.Request(
        base + "/api/auth/login?username=" + urllib.parse.quote(secret["username"]),
        data=b"",
        headers={"X-Password": urllib.parse.quote(secret["password"])},
    )
    client.open(login, timeout=20).read()
    print("PASS authenticated session")
    run = "Acceptance-" + uuid.uuid4().hex[:10]
    music = root / "Music" / run
    inbox = root / "Inbox" / run
    for folder in (music / "nested", music / "destination", inbox):
        folder.mkdir(parents=True)
    content = b"Disposable acceptance file with Unicode: \xc3\xa6\xc3\xb8\xc3\xa5\n"
    (music / "original.txt").write_bytes(content)
    (music / "nested" / "copy.txt").write_bytes(content)
    changed = bytearray(content)
    changed[-1] = ord("!")
    (music / "nested" / "different.txt").write_bytes(changed)
    start = "/" + run + "/"

    def job(kind, items, destination=None, expected="completed"):
        submitted = request("/api/organizer/jobs", {
            "kind": kind, "items": items,
            **({"destination": destination} if destination else {}),
        })
        until = time.monotonic() + 30
        result = submitted
        while result["status"] in ("queued", "running") and time.monotonic() < until:
            time.sleep(0.05)
            result = request("/api/organizer/jobs/" + submitted["id"])
        assert result["status"] == expected, result
        return result

    original = {"source": "Music", "path": start + "original.txt"}
    job("copy", [original], {"source": "Inbox", "path": start})
    assert (inbox / "original.txt").read_bytes() == content
    assert (music / "original.txt").read_bytes() == content
    print("PASS durable cross-source copy")
    moved = {"source": "Inbox", "path": start + "original.txt"}
    job("move", [moved], {"source": "Music", "path": start + "destination/"})
    assert not (inbox / "original.txt").exists()
    assert (music / "destination" / "original.txt").read_bytes() == content
    print("PASS durable move and index refresh")
    moved = {"source": "Music", "path": start + "destination/original.txt"}
    job("trash", [moved])
    assert not (music / "destination" / "original.txt").exists()
    deleted = next(t for t in request("/api/organizer/trash") if t["path"] == moved["path"])
    assert deleted["status"] == "ready"
    restored = {"source": "Music", "path": moved["path"], "trashId": deleted["id"]}
    (music / "destination" / "original.txt").write_bytes(b"Do not overwrite this")
    job("restore", [restored], expected="failed")
    assert (music / "destination" / "original.txt").read_bytes() == b"Do not overwrite this"
    (music / "destination" / "original.txt").unlink()
    job("restore", [restored])
    assert (music / "destination" / "original.txt").read_bytes() == content
    print("PASS recoverable trash, restore and conflict protection")

    request("/api/library/scans", {"source": "Music", "path": start})
    query = "/api/library?" + urllib.parse.urlencode({"source": "Music", "path": start, "limit": 100})
    until = time.monotonic() + 60
    while True:
        result = request(query)
        if result.get("scan", {}).get("status") not in ("queued", "running"):
            break
        assert time.monotonic() < until, "library scan timed out"
        time.sleep(0.1)
    assert result["scan"]["status"] == "completed", result["scan"]
    digest = hashlib.sha256(content).hexdigest()
    group = next(g for g in result["duplicates"] if g["sha256"] == digest)
    assert len(group["files"]) == 3, group
    assert all("different.txt" not in f["path"] for f in group["files"])
    print("PASS recursive full-file SHA-256 duplicate scan")
    request("/api/library/tags", {**original, "tags": ["tagged-personal-proof", "personal"]}, "PATCH")
    tagged = request(query + "&q=tagged-personal-proof")
    assert tagged["total"] == 1 and "tagged-personal-proof" in tagged["items"][0]["tags"]
    print("PASS persisted personal tags and indexed search")
    extra = {"source": "Music", "path": start + "nested/copy.txt", "expectedSha256": digest, "keeper": original}
    job("trash", [extra])
    assert (music / "original.txt").read_bytes() == content
    assert not (music / "nested" / "copy.txt").exists()
    print("PASS duplicate cleanup retains verified keeper")
    (music / "nested" / "copy.txt").write_bytes(changed)
    job("trash", [extra], expected="failed")
    assert (music / "nested" / "copy.txt").read_bytes() == changed
    print("PASS stale duplicate cleanup refuses changed content")
    try:
        request("/api/organizer/jobs", {"kind": "trash", "items": [{"source": "Music", "path": "/../escape"}]})
        raise AssertionError("traversal accepted")
    except urllib.error.HTTPError as error:
        assert error.code in (400, 403), error.code
    print("PASS traversal refused")
    print("PASS complete HTTP acceptance workflow: " + run)


if __name__ == "__main__":
    main()
