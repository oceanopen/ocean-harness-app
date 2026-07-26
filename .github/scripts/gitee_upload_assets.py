#!/usr/bin/env python3
"""上传构建资产到 Gitee Release（幂等，curl 单次上传，失败即红）。

由 .github/workflows/release-gitee-sync.yml 在 gitee_create_release.py 之后调用。
遍历 ./assets/*（跳过 GitHub 版 latest.json 与已存在同名附件），逐个用 curl 上传到
Gitee Release 的 attach_files。

设计原则：「HTTP/JSON 用 python、文件上传用 curl」—— 上传走 curl 获得
总超时(--max-time) + 慢速识别(--speed-limit) + 流式不占内存 + 久经考验的 multipart；
python 负责去重 / 遍历 / 日志 / 失败汇总。

失败语义：任一文件失败（含超时/慢速中止）→ exit 非 0 → 步骤红。不重试。
唯一非致命点：「拉已有附件去重」的 fetch 失败 → 仅 warning 降级（非核心优化）。
实时日志：stdout 行缓冲。

输入（环境变量）：
  GITEE_OWNER   Gitee 组织/用户名（job 级）
  GITEE_REPO    Gitee 仓库名（job 级）
  GITEE_TOKEN   Gitee 个人访问令牌（step 级）
  RELEASE_ID    Gitee Release ID（step 级，由 gitee_create_release.py 输出）

输入（文件）：
  ./assets/*    gh release download 下载的全部资产
"""
import glob
import json
import os
import subprocess
import sys
import time
import urllib.request

# 行缓冲：每行 print 立即 flush 到 Actions 日志，实时可见（CI 非 TTY 默认块缓冲）
sys.stdout.reconfigure(line_buffering=True)

API_TIMEOUT = 60              # Gitee API（dedup 查询）超时（秒）
UPLOAD_MAX_TIME = "3600"       # curl 总超时（秒）—— 整个上传必须在此时长内完成
UPLOAD_SPEED_LIMIT = "1024"  # curl：平均速度低于此值（字节/秒，1KB/s）…
UPLOAD_SPEED_TIME = "60"      # …持续 60s 即中止 → 快速识别 Gitee 彻底卡死


def _read_json(resp):
    """读取响应体并解析 JSON。空 body / 非 JSON（如 Gitee 偶发的 HTML 错误页）→ 返回 None。"""
    raw = resp.read().decode("utf-8", errors="replace")
    if not raw.strip():
        return None
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        return None


def upload_one(upload_url, token, name, path, idx, total):
    """用 curl 上传单个文件（单次，不重试）。返回 True 成功 / False 失败。"""
    size = os.path.getsize(path)
    print(f"  [{idx}/{total}] {name} ({size / 1_000_000:.1f} MB) uploading…")
    t0 = time.time()
    # access_token 放 -F form field（Gitee v5 鉴权坑）；file=@path 流式读取，不占内存
    cmd = [
        "curl", "-fsS",
        "--max-time", UPLOAD_MAX_TIME,
        "--speed-limit", UPLOAD_SPEED_LIMIT,
        "--speed-time", UPLOAD_SPEED_TIME,
        "-o", "/dev/null",  # 丢弃响应体
        "--write-out", "%{http_code} %{time_total} %{speed_upload}",
        "-X", "POST", upload_url,
        "-F", f"access_token={token}",
        "-F", f"name={name}",
        "-F", f"file=@{path}",
    ]
    result = subprocess.run(cmd, capture_output=True, text=True)
    elapsed = time.time() - t0
    if result.returncode == 0:
        parts = result.stdout.strip().split()
        if len(parts) >= 3:
            stats = f"HTTP {parts[0]}, {float(parts[1]):.1f}s, {float(parts[2]) / 1e6:.2f} MB/s"
        else:
            stats = result.stdout.strip() or "ok"
        print(f"  [{idx}/{total}] {name} uploaded ({stats})")
        return True
    err_tail = result.stderr.strip().splitlines()[-1] if result.stderr.strip() else f"curl exit {result.returncode}"
    print(f"::error::[{idx}/{total}] {name} failed after {elapsed:.1f}s (curl exit {result.returncode}): {err_tail}")
    return False


def main() -> None:
    owner = os.environ["GITEE_OWNER"]
    repo = os.environ["GITEE_REPO"]
    token = os.environ["GITEE_TOKEN"]
    release_id = os.environ["RELEASE_ID"]
    base = f"https://gitee.com/api/v5/repos/{owner}/{repo}"
    upload_url = f"{base}/releases/{release_id}/attach_files"
    print(f"Upload assets to Gitee Release {release_id} via curl "
          f"(--max-time {UPLOAD_MAX_TIME}s, --speed-limit {UPLOAD_SPEED_LIMIT}B/s, no retry)")

    # 幂等：拉已有附件名集合，re-run 时跳过已存在同名文件。
    # 此 fetch 为非核心优化 —— 失败仅降级为不去重，不废整个上传。
    existing = []
    print("Fetching existing assets for dedup…")
    try:
        with urllib.request.urlopen(
            f"{base}/releases/{release_id}?access_token={token}", timeout=API_TIMEOUT
        ) as r:
            rel = _read_json(r)
        if not isinstance(rel, dict):
            raise RuntimeError(f"non-object response: {rel!r:.100}")
        assets = rel.get("assets") or rel.get("attach_files") or []
        existing = sorted(a.get("name", "") for a in assets if isinstance(a, dict))
        if existing:
            print(f"  {len(existing)} existing asset(s), will skip:")
            for n in existing:
                print(f"    - {n}")
        else:
            print("  no existing asset")
    except Exception as e:
        print(f"::warning::dedup fetch failed ({type(e).__name__}: {e}), dedup disabled")
        existing = []

    existing_set = set(existing)
    paths = [p for p in sorted(glob.glob("./assets/*")) if os.path.basename(p) != "latest.json"]
    total = len(paths)
    total_size = sum(os.path.getsize(p) for p in paths)
    print(f"{total} asset(s) to upload, ~{total_size / 1_000_000:.1f} MB total")

    failed = []
    for i, path in enumerate(paths, 1):
        name = os.path.basename(path)
        if name in existing_set:
            print(f"  [{i}/{total}] {name} skipped (already attached)")
            continue
        if not upload_one(upload_url, token, name, path, i, total):
            failed.append(name)

    if failed:
        print(f"::error::{len(failed)}/{total} asset(s) failed: {failed}")
        raise SystemExit(1)
    print(f"All {total} asset(s) uploaded")


if __name__ == "__main__":
    # 真异常直接抛出 → exit 非 0 → 步骤红，绝不静默吞掉
    main()
