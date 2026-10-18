#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
org_sync.py — kaula-universe 组织仓库在 GitHub 与 Gitee 之间的双向互同步
============================================================

职责
----
把组织内仓库在 GitHub(kaula-universe) 与 Gitee(kaula-universe) 之间做
「内容级双向互同步，保留完整 git 历史」，并自动覆盖 新增 / 修改 / 删除。

规则说明
--------
1) 新增(ADD)    ：一端存在、另一端不存在 -> 在另一端创建同名校验并镜像推送。
2) 修改(SYNC)  ：两端都存在 -> 拉取两端最新，做快进/合并收敛，再推送回两端。
                  分支冲突时尝试 --allow-unrelated-histories 自动合并；合并不了则
                  仅报告、不覆盖，绝不静默丢数据。
3) 删除(DELETE) ：一端存在、另一端不存在 -> 先备份到本地 artifact，再从「存在侧」
                  删除。受 PROTECT 列表保护的仓库绝不删除。
                  注意：纯 diff 无法区分「另一端新增」还是「本侧删除」，因此删除
                  以 GITEE 为权威(这些仓始于 Gitee)。仅当 GitHub 侧存在而 Gitee
                  侧不存在时才删 GitHub 侧；Gitee 侧只会被新增、不会被自动删。

安全设计
--------
- 默认 dry-run(--dry-run)，只读不写；加 --apply 才真正执行变更。
- 任何删除前先 git clone --mirror 备份到 artifacts/<repo>-<ts>.git。
- DELETE 类动作会打印清单，需 --apply 才执行。
- 主仓列入 PROTECT 白名单，永不参与组织级自动删除。

凭证
----
环境变量(不落盘)：
  GH_TOKEN       GitHub PAT  (repo / admin:org / delete_repo / workflow)
  GITEE_TOKEN    Gitee PAT    (projects 权限)
  GITEE_USER     Gitee 用户名 (如 zhe116)
可选配置：
  GH_ORG / GITEE_ORG (默认 kaula-universe)
  --exclude 排除的仓库名，逗号分隔

用法(在 GitHub Actions 中由 org-sync.yml 调用)
----
  python3 scripts/org_sync.py --dry-run
  python3 scripts/org_sync.py --apply
"""

import argparse
import json
import os
import shutil
import stat
import subprocess
import sys
import time
import urllib.request
import urllib.error
from datetime import datetime, timezone

# --------------------------------------------------------------------------
# 常量与配置
# --------------------------------------------------------------------------
GH_ORG = os.environ.get("GH_ORG", "kaula-universe")
GITEE_ORG = os.environ.get("GITEE_ORG", "kaula-universe")
GITEE_USER = os.environ.get("GITEE_USER", "zhe116")

GH_API = "https://api.github.com"
GITEE_API = "https://gitee.com/api/v5"

# 受保护仓库：永不参与组织级自动删除
PROTECT = {"Kaula_language_compiler", "kaula", "termus", "kaulaweb"}

WORKDIR = os.environ.get("ORG_SYNC_WORKDIR", "/tmp/org-sync")
ARTIFACT_DIR = os.environ.get("ORG_SYNC_ARTIFACT_DIR", "/tmp/org-sync-artifacts")

# GitHub 创建仓库时是否私有（默认继承 Gitee 端；无 Gitee 端则读 GH_PRIVATE，默认公开）
GH_PRIVATE = os.environ.get("GH_PRIVATE", "false").lower() == "true"

# 权威侧(PRIMARY)：决定「单侧存在」的仓库如何处理。
#   primary=gitee(默认)：Gitee 单侧存在 -> 新增到 GitHub；GitHub 单侧存在 -> 视为在
#                        Gitee 已被删除，备份后从 GitHub 删除。
#   primary=github     ：语义相反。
# 这是纯 diff 同步器无法自动识别「新增 vs 删除」意图时的必要 tiebreaker。
PRIMARY = os.environ.get("ORG_SYNC_PRIMARY", "gitee").lower()


def log(msg):
    print(f"[org-sync] {msg}", flush=True)


def now_ts():
    return datetime.now(timezone.utc).strftime("%Y%m%d-%H%M%S")


# --------------------------------------------------------------------------
# HTTP 请求（stdlib，避免依赖 requests）
# --------------------------------------------------------------------------
def http_json(method, url, headers=None, body=None, ok_404=False):
    """send JSON http request, return (status, parsed). Supports bearer & query token."""
    data = None
    if body is not None:
        data = json.dumps(body).encode("utf-8")
        headers = dict(headers or {})
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(url, data=data, headers=headers or {}, method=method)
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            raw = resp.read().decode("utf-8")
            return resp.status, (json.loads(raw) if raw else None)
    except urllib.error.HTTPError as e:
        raw = e.read().decode("utf-8", "replace")
        if ok_404 and e.code == 404:
            return e.code, None
        try:
            parsed = json.loads(raw)
        except Exception:
            parsed = {"message": raw[:500]}
        log(f"HTTP {e.code} {method} {url} -> {json.dumps(parsed, ensure_ascii=False)[:300]}")
        return e.code, parsed
    except Exception as e:  # noqa: BLE001
        log(f"NETERR {method} {url} -> {e}")
        return -1, {"message": str(e)}


def gh_headers():
    token = os.environ["GH_TOKEN"]
    return {
        "Authorization": f"Bearer {token}",
        "Accept": "application/vnd.github+json",
        "X-GitHub-Api-Version": "2022-11-28",
    }


def gitee_query():
    return {"access_token": os.environ["GITEE_TOKEN"]}


# --------------------------------------------------------------------------
# 仓库清单获取
# --------------------------------------------------------------------------
def list_github_repos():
    repos, page, per = [], 1, 100
    while True:
        st, data = http_json(
            "GET",
            f"{GH_API}/orgs/{GH_ORG}/repos?per_page={per}&page={page}",
            headers=gh_headers(),
        )
        if st != 200 or data is None:
            log(f"GitHub 仓库清单获取失败 status={st}")
            break
        repos.extend(data)
        if len(data) < per:
            break
        page += 1
    result = {}
    for r in repos:
        name = r["name"]
        result[name.lower()] = {
            "name": name,
            "private": bool(r.get("private")),
            "url": r.get("clone_url"),
            "default_branch": r.get("default_branch", "master"),
        }
    return result


def list_gitee_repos():
    repos, page, per = [], 1, 100
    while True:
        q = gitee_query()
        st, data = http_json(
            "GET",
            f"{GITEE_API}/orgs/{GITEE_ORG}/repos?per_page={per}&page={page}&access_token={q['access_token']}",
        )
        if st != 200 or data is None:
            log(f"Gitee 仓库清单获取失败 status={st}")
            break
        repos.extend(data)
        if len(data) < per:
            break
        page += 1
    result = {}
    for r in repos:
        name = r["name"]
        result[name.lower()] = {
            "name": name,
            "private": bool(r.get("private")),
            "url": r.get("html_url"),
            "default_branch": r.get("default_branch", "master"),
            "ssh_url": r.get("ssh_url"),
        }
    return result


# --------------------------------------------------------------------------
# git 工具函数
# --------------------------------------------------------------------------
def run_git(args, cwd=None, env=None):
    e = dict(os.environ)
    e["GIT_TERMINAL_PROMPT"] = "0"
    e["GIT_SSL_NO_VERIFY"] = "1"
    e["GIT_SSL_NO_REVOKE"] = "1"
    if env:
        e.update(env)
    r = subprocess.run(["git"] + args, cwd=cwd, env=e, capture_output=True, text=True)
    return r.returncode, r.stdout.strip(), r.stderr.strip()


def gh_auth_url(repo_url, repo_name=None):
    """给 GitHub https clone_url 注入 token。"""
    token = os.environ["GH_TOKEN"]
    if repo_url:
        # 形如 https://github.com/org/repo.git
        return repo_url.replace("https://", f"https://x-access-token:{token}@")
    return f"https://x-access-token:{token}@github.com/{GH_ORG}/{repo_name}.git"


def gitee_auth_url(repo_url=None, repo_name=None):
    token = os.environ["GITEE_TOKEN"]
    if repo_url:
        return repo_url.replace("https://", f"https://{GITEE_USER}:{token}@")
    return f"https://{GITEE_USER}:{token}@gitee.com/{GITEE_ORG}/{repo_name}.git"


def force_rmtree(path):
    """删除目录树；Windows 下 git 生成的文件多为只读，先清除只读位再删。"""
    if not os.path.exists(path):
        return
    def _onerr(fn, p, exc):
        try:
            os.chmod(p, stat.S_IWRITE)
            fn(p)
        except Exception:  # noqa: BLE001
            pass
    shutil.rmtree(path, onerror=_onerr)


def clone_mirror(url, dest):
    force_rmtree(dest)
    os.makedirs(os.path.dirname(dest), exist_ok=True)
    rc, out, err = run_git(["clone", "--mirror", url, dest])
    return rc, err


# --------------------------------------------------------------------------
# 创建仓库（API）
# --------------------------------------------------------------------------
def create_github_repo(name, private):
    body = {"name": name, "auto_init": False, "private": private,
            "description": f"Mirrored from Gitee org {GITEE_ORG}"}
    st, data = http_json("POST", f"{GH_API}/orgs/{GH_ORG}/repos",
                         headers=gh_headers(), body=body)
    return st in (200, 201), data


def create_gitee_repo(name, private):
    body = {"name": name, "private": str(private).lower(),
            "description": "Mirrored from GitHub org kaula-universe"}
    url = f"{GITEE_API}/orgs/{GITEE_ORG}/repos?access_token={os.environ['GITEE_TOKEN']}"
    st, data = http_json("POST", url, headers=None, body=body)
    return st in (200, 201), data


def delete_github_repo(name):
    st, data = http_json("DELETE", f"{GH_API}/repos/{GH_ORG}/{name}", headers=gh_headers())
    return st in (200, 204), data


# --------------------------------------------------------------------------
# 目录/分支收敛：把 src_ref 的 ref 推到 dst，若 dst 落后则快进；若分叉则合并
# --------------------------------------------------------------------------
def is_ancestor(cwd, anc, desc):
    rc, _, _ = run_git(["merge-base", "--is-ancestor", anc, desc], cwd=cwd)
    return rc == 0


def refs(cwd, pattern):
    """返回匹配 pattern 的完整 refname 列表。"""
    rc, out, _ = run_git(["for-each-ref", "--format=%(refname)", pattern], cwd=cwd)
    return [r.strip() for r in (out or "").split("\n") if r.strip()]


def sync_repo(meta, exclude, apply=True):
    """同步单个已在两端都存在的仓库。meta = 统一 dict(name, gh, gee)。

    apply=True 才真正 push；否则只克隆对比、打印将执行的收敛动作（dry-run 安全）。

    命名空间约定：
      refs/heads/*           -> Gitee 侧分支
      refs/remotes/gh/heads/*-> GitHub 侧分支
      refs/tags/*            -> Gitee 侧 tag
      refs/remotes/gh/tags/* -> GitHub 侧 tag
    """
    name = meta["name"]
    if name.lower() in exclude:
        log(f"[SKIP] {name} 在 --exclude 列表")
        return "skip"

    os.makedirs(WORKDIR, exist_ok=True)
    tmp = os.path.join(WORKDIR, f"repo-{name}.git")
    gh_url = gh_auth_url(meta["gh"]["url"], name)
    gee_url = gitee_auth_url(meta["gee"].get("url"), name)

    def do_push(args, label):
        if apply:
            rc, _, err = run_git(["push"] + args, cwd=tmp)
            if rc != 0:
                # tag 已存在同类推送是无害幂等，视为成功
                if "already exists" in err:
                    log(f"  (noop) {label}: 目标已存在，跳过")
                    return True
                log(f"[PUSH FAIL] {name}: {label}: {err[:300]}")
                return False
        else:
            log(f"  (dry-run) push {label}")
        return True

    # 以 Gitee 为基准克隆 mirror，再把 GitHub 拉成 gh/ 前缀引用（命名空间隔离）
    rc, err = clone_mirror(gee_url, tmp)
    if rc != 0:
        log(f"[WARN] 克隆 Gitee 失败({err})，改用 GitHub 作基准")
        rc, err = clone_mirror(gh_url, tmp)
        if rc != 0:
            log(f"[FAIL] {name} 无法克隆任一端: {err}")
            return "fail"
    # 移除 mirror 自带的 origin remote：它的 fetch refspec 是 +refs/*:refs/*，
    # 会覆盖 refs/remotes/gh/* 造成 worktree add 的 tracking 歧义。
    run_git(["remote", "remove", "origin"], cwd=tmp)
    run_git(["remote", "add", "gh", gh_url], cwd=tmp)
    run_git(["fetch", "gh",
             "+refs/heads/*:refs/remotes/gh/heads/*",
             "+refs/tags/*:refs/remotes/gh/tags/*"], cwd=tmp)

    changed = False

    # ---- 分支：收敛两端同名分支 ----
    gitee_heads = refs(tmp, "refs/heads/")
    gh_heads = refs(tmp, "refs/remotes/gh/heads/")
    gh_head_map = {h.split("gh/heads/", 1)[1]: h for h in gh_heads}

    for g_head in gitee_heads:
        gb = g_head[len("refs/heads/"):]
        gh_b = gh_head_map.get(gb)
        if gh_b is None:
            # 仅 Gitee 有 -> 推送到 GitHub
            if do_push(["gh", f"{g_head}:refs/heads/{gb}"], f"{name} 新增分支 {gb} -> GH"):
                log(f"[SYNC->GH] {name}: 新增分支 {gb}")
                changed = True
            continue
        if is_ancestor(tmp, g_head, gh_b):
            # GitHub 领先 -> 把 GitHub 推回 Gitee（快进）
            if do_push([gee_url, f"{gh_b}:refs/heads/{gb}"], f"{name} 快进分支 {gb} -> GEE"):
                log(f"[SYNC->GEE] {name}: 分支 {gb} 快进到 GitHub 端")
                changed = True
        elif is_ancestor(tmp, gh_b, g_head):
            # Gitee 领先 -> 把 Gitee 推到 GitHub（快进）
            if do_push(["gh", f"{g_head}:refs/heads/{gb}"], f"{name} 快进分支 {gb} -> GH"):
                log(f"[SYNC->GH] {name}: 分支 {gb} 快进到 Gitee 端")
                changed = True
        else:
            # 分叉：在临时 worktree 中尝试自动合并（--allow-unrelated-histories）
            # 先确保 runner 有提交身份（merge 需要创建 merge commit）
            run_git(["config", "user.name", "kaula-sync-bot"], cwd=tmp)
            run_git(["config", "user.email", "bot@kaula.local"], cwd=tmp)
            mb = f"merge-{gb}-{now_ts()}"
            wt = os.path.join(WORKDIR, f"wt-{name}-{mb}")
            # 先解析出目标 commit SHA，避免用 remote-tracking ref 造成 tracking 歧义
            rc, sha, _ = run_git(["rev-parse", gh_b], cwd=tmp)
            target = sha if (rc == 0 and sha) else gh_b
            rc, _, err = run_git(["worktree", "add", "-b", mb, wt, target], cwd=tmp)
            if rc != 0:
                log(f"[MERGE SKIP] {name}: 分支 {gb} worktree 创建失败: {err[:300]}")
                continue
            rc, _, err = run_git(["merge", gb, "--allow-unrelated-histories", "--no-edit"], cwd=wt)
            if rc == 0:
                ok1 = do_push(["gh", f"refs/heads/{mb}:refs/heads/{gb}"],
                              f"{name} 合并分支 {gb} -> GH")
                ok2 = do_push([gee_url, f"refs/heads/{mb}:refs/heads/{gb}"],
                              f"{name} 合并分支 {gb} -> GEE")
                if ok1 and ok2:
                    log(f"[MERGE OK] {name}: 分支 {gb} 两端分叉已自动合并并推送")
                    changed = True
            else:
                # 需要人工解决的才是 CONFLICT；环境性失败（如需身份）单独报告
                low = err.lower()
                if "committer identity unknown" in low or "please tell me who you are" in low \
                        or "user.name" in low or "user.email" in low:
                    log(f"[MERGE ENV] {name}: 分支 {gb} 缺少提交身份，跳过: {err[:300]}")
                else:
                    log(f"[MERGE CONFLICT] {name}: 分支 {gb} 存在冲突，跳过(不覆盖): {err[:300]}")
            run_git(["worktree", "remove", "--force", wt], cwd=tmp)

    # ---- 仅 GitHub 有分支 -> 补到 Gitee ----
    gitee_head_set = set(gitee_heads)
    for gh_b in gh_heads:
        gb = gh_b.split("gh/heads/", 1)[1]
        if f"refs/heads/{gb}" not in gitee_head_set:
            if do_push([gee_url, f"{gh_b}:refs/heads/{gb}"], f"{name} 新增分支 {gb} -> GEE"):
                log(f"[SYNC->GEE] {name}: 新增分支 {gb}")
                changed = True

    # ---- tag：并集推送（不删除） ----
    for t in refs(tmp, "refs/tags/"):
        do_push(["gh", f"{t}:refs/tags/{t.split('refs/tags/',1)[1]}"], f"{name} tag -> GH")
    gh_tag_map = {h.split("gh/tags/", 1)[1]: h for h in refs(tmp, "refs/remotes/gh/tags/")}
    for tname, tfull in gh_tag_map.items():
        if f"refs/tags/{tname}" not in gitee_head_set and tname not in [t.split("refs/tags/", 1)[1] for t in refs(tmp, "refs/tags/")]:
            if do_push([gee_url, f"{tfull}:refs/tags/{tname}"], f"{name} 新增 tag {tname} -> GEE"):
                log(f"[SYNC->GEE] {name}: 新增 tag {tname}")
                changed = True

    log(f"[SYNC DONE] {name} changed={changed}" if changed else f"[SYNC NOOP] {name} 两端一致")
    return "sync"


# --------------------------------------------------------------------------
# 备份并删除
# --------------------------------------------------------------------------
def backup_and_delete(meta, side, exclude, apply):
    name = meta["name"]
    api_name = meta[side]["name"]
    if name.lower() in exclude or name in PROTECT:
        log(f"[SKIP-DELETE] {name} 受保护或在排除列表，跳过删除")
        return "protected"

    artifact_dir = os.path.join(ARTIFACT_DIR, name)
    os.makedirs(artifact_dir, exist_ok=True)
    bak = os.path.join(artifact_dir, f"{name}-{now_ts()}.git")
    url = gh_auth_url(meta[side]["url"], api_name) if side == "gh" else gitee_auth_url(meta[side]["url"], api_name)
    rc, err = clone_mirror(url, bak)
    if rc != 0:
        log(f"[FAIL] {name} 备份失败({err})，为保数据安全已中止删除")
        return "fail"

    if not apply:
        log(f"[DRY-RUN DELETE] {name} 将删除 GitHub 侧(已备份 {bak})")
        return "would-delete"

    if side == "gh":
        ok, _ = delete_github_repo(api_name)
    else:
        # Gitee 侧不自动删（权威侧），此处不实现
        ok, _ = False, None
    log(f"[DELETE] {name} GitHub 侧删除 {'成功' if ok else '失败'}")
    return "deleted" if ok else "fail"


# --------------------------------------------------------------------------
# 主流程
# --------------------------------------------------------------------------
def main():
    ap = argparse.ArgumentParser(description="kaula-universe 组织仓库双向同步")
    ap.add_argument("--apply", action="store_true", help="真正执行变更（默认 dry-run）")
    ap.add_argument("--exclude", default="", help="逗号分隔的仓库名（同步时跳过）")
    args = ap.parse_args()
    apply = args.apply
    exclude = {e.strip().lower() for e in args.exclude.split(",") if e.strip()}

    log(f"===== kaula-universe 组织同步 {'(APPLY)' if apply else '(DRY-RUN)'} =====")
    log(f"GitHub org={GH_ORG}   Gitee org={GITEE_ORG}")

    gh = list_github_repos()
    gee = list_gitee_repos()
    log(f"GitHub 仓库: {len(gh)} 个 -> {sorted(gh)}")
    log(f"Gitee 仓库: {len(gee)} 个 -> {sorted(gee)}")

    all_keys = set(gh) | set(gee)

    # 归档备份目录
    os.makedirs(ARTIFACT_DIR, exist_ok=True)

    for key in sorted(all_keys):
        gh_info = gh.get(key)
        gee_info = gee.get(key)
        disp = gh_info["name"] if gh_info else (gee_info["name"] if gee_info else key)

        # 两端都有 -> 内容级收敛（保留历史）
        if gh_info and gee_info:
            meta = {"name": disp, "gh": gh_info, "gee": gee_info}
            sync_repo(meta, exclude, apply=apply)
            continue

        # 单侧存在 -> 按 PRIMARY 权威侧决定「新增」还是「删除」
        primary_side, secondary_side = ("gee", "gh") if PRIMARY == "gitee" else ("gh", "gee")
        on_primary = gee_info if primary_side == "gee" else gh_info
        on_secondary = gh_info if secondary_side == "gh" else gee_info
        present = on_primary or on_secondary

        # 权威侧存在、另一侧缺失 -> 新增到缺失的一侧
        if on_primary and not on_secondary:
            if not apply:
                log(f"[DRY-RUN ADD->{secondary_side.upper()}] {disp} 另一侧缺失，将创建并镜像")
                continue
            ok = create_github_repo(disp, present.get("private", False)) if secondary_side == "gh" \
                else create_gitee_repo(disp, present.get("private", False))
            ok = ok[0]
            if not ok:
                log(f"[FAIL] 创建 {secondary_side} 仓库 {disp} 失败")
                continue
            src_url = gitee_auth_url(gee_info.get("url"), disp) if primary_side == "gee" \
                else gh_auth_url(gh_info.get("url"), disp)
            dest_url = gh_auth_url(None, disp) if secondary_side == "gh" else gitee_auth_url(None, disp)
            tmp = os.path.join(WORKDIR, f"add-{disp}.git")
            rc, err = clone_mirror(src_url, tmp)
            if rc == 0:
                run_git(["push", "--mirror", dest_url], cwd=tmp)
                log(f"[ADD->{secondary_side.upper()}] {disp} 已创建并镜像")
            else:
                log(f"[FAIL] 克隆 {primary_side} {disp} 失败: {err}")

        # 单侧(次要侧)存在、权威侧缺失 -> 视为在权威侧已删除，备份后从次要侧删除
        elif on_secondary and not on_primary:
            meta = {"name": disp, primary_side: None, secondary_side: on_secondary}
            backup_and_delete(meta, secondary_side, exclude, apply)

    log("===== 同步流程结束 =====")
    log(f"备份产物目录: {ARTIFACT_DIR}")


if __name__ == "__main__":
    main()
