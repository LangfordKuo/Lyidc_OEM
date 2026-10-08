#!/usr/bin/env python3
"""交付资产自检：Release 工作流结构断言 + Dockerfile/compose 形式审查。

在 CI 之前把「YAML 写错了 / 少了个 job / 冒烟步骤被删掉 / Dockerfile 少了 HEALTHCHECK」
这类问题拦下来，避免推 tag 后才发现流水线跑不通（tag 已推、Release 已发，返工代价高）。

用法：
    python scripts/check_release_workflow.py              # 自动选择 YAML 解析器
    python scripts/check_release_workflow.py --parser mini  # 强制用内置解析器（无 PyYAML 环境）

检查内容：
    A. 触发条件：push tag v*、workflow_dispatch 的 dry_run 布尔入参
    B. jobs/needs：frontend → build-packages → release；docker → smoke
    C. build-packages：三平台 matrix、setup-go 缓存、复用前端 artifact、打包脚本调用、上传 artifact
    D. release：仅在 tag 且非 dry_run 时执行、contents:write、附全部产物、自动生成更新说明
    E. docker：packages:write、buildx/qemu、双镜像、latest 只在 tag 上给、GHA 缓存、dry_run 不推送
    F. smoke：mysql:5.7 service、未安装态断言、安装向导全流程、就绪态、前端 SPA 挂载点、
       反代链路、healthcheck healthy、失败 dump 日志
    G. 所有 run 脚本过 shell 语法检查（sh -n）
    H. Dockerfile.backend / Dockerfile.frontend：多阶段、TARGETARCH 位置、HEALTHCHECK、运行时基础镜像
    I. deploy/docker-compose.yml：三服务、健康检查链、数据卷、对外端口

退出码：0 = 全部通过；1 = 有失败项（逐条打印）。
"""
from __future__ import annotations

import os
import re
import subprocess
import sys
import tempfile

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
WORKFLOW = os.path.join(ROOT, ".github", "workflows", "release.yml")
COMPOSE = os.path.join(ROOT, "deploy", "docker-compose.yml")
DOCKERFILE_BACKEND = os.path.join(ROOT, "Dockerfile.backend")
DOCKERFILE_FRONTEND = os.path.join(ROOT, "Dockerfile.frontend")
ENTRYPOINT = os.path.join(ROOT, "deploy", "docker", "backend-entrypoint.sh")
NGINX_TEMPLATE = os.path.join(ROOT, "deploy", "docker", "nginx-default.conf.template")

# ---------------------------------------------------------------- 内置 YAML 解析器
# CI 环境不保证装了 PyYAML，故带一个只覆盖「工作流/编排文件常用子集」的解析器：
# 块映射、块序列（含「序列项即映射」）、块标量（| 与 > 系列）、流序列 [a, b]、引号标量、
# 注释与空行。不支持锚点/别名/多文档/复杂键——这些在本仓库的交付文件里本就不该出现。


class MiniYamlError(Exception):
    pass


def _mini_scalar(text: str):
    text = text.strip()
    if text.startswith('"') and text.endswith('"') and len(text) >= 2:
        return _unescape(text[1:-1])
    if text.startswith("'") and text.endswith("'") and len(text) >= 2:
        return text[1:-1].replace("''", "'")
    if text.startswith("[") and text.endswith("]"):
        return _split_flow(text[1:-1])
    if text.startswith("{") and text.endswith("}"):
        out = {}
        for part in _split_flow(text[1:-1]):
            if isinstance(part, str) and ":" in part:
                key, _, value = part.partition(":")
                out[key.strip()] = _mini_scalar(value)
        return out
    lowered = text.lower()
    if lowered in ("true", "yes", "on"):
        return True
    if lowered in ("false", "no", "off"):
        return False
    if lowered in ("null", "~", ""):
        return None
    if re.fullmatch(r"-?\d+", text):
        return int(text)
    if re.fullmatch(r"-?\d+\.\d+", text):
        return float(text)
    return text


def _unescape(text: str) -> str:
    out = []
    i = 0
    while i < len(text):
        if text[i] == "\\" and i + 1 < len(text):
            out.append({"n": "\n", "t": "\t", '"': '"', "\\": "\\"}.get(text[i + 1], text[i + 1]))
            i += 2
        else:
            out.append(text[i])
            i += 1
    return "".join(out)


def _split_flow(text: str):
    parts, buf, quote, i = [], "", None, 0
    while i < len(text):
        ch = text[i]
        if quote:
            if ch == "\\" and quote == '"' and i + 1 < len(text):
                buf += ch + text[i + 1]
                i += 2
                continue
            if ch == quote:
                quote = None
            buf += ch
        elif ch in "\"'":
            quote = ch
            buf += ch
        elif ch == ",":
            parts.append(_mini_scalar(buf))
            buf = ""
        else:
            buf += ch
        i += 1
    if buf.strip():
        parts.append(_mini_scalar(buf))
    return parts


def _is_dash(text: str) -> bool:
    return text == "-" or text.startswith("- ")


def _map_split(text: str):
    """把 'key: value' 拆开；不是映射行时返回 None。"""
    quote, i = None, 0
    while i < len(text):
        ch = text[i]
        if quote:
            if ch == "\\" and quote == '"':
                i += 2
                continue
            if ch == quote:
                quote = None
        elif ch in "\"'":
            quote = ch
        elif ch == ":" and (i + 1 == len(text) or text[i + 1] == " "):
            key = text[:i].strip()
            if key:
                return key, text[i + 1:].strip()
        i += 1
    return None


class _MiniParser:
    def __init__(self, lines):
        self.lines = lines
        self.i = 0

    def peek(self):
        return self.lines[self.i] if self.i < len(self.lines) else None

    def parse_block(self, parent_indent: int):
        cur = self.peek()
        if cur is None or cur[0] <= parent_indent:
            return None
        indent, text = cur
        if _is_dash(text.strip()):
            return self.parse_seq(indent)
        return self.parse_map(indent)

    def parse_seq(self, indent: int):
        items = []
        while True:
            cur = self.peek()
            if cur is None:
                break
            cur_indent, text = cur
            if cur_indent != indent or not _is_dash(text.strip()):
                break
            rest = text.strip()[1:].strip()
            if rest == "":
                self.i += 1
                items.append(self.parse_block(indent))
                continue
            if _map_split(rest):
                sub = [(indent + 2, rest)]
                self.i += 1
                while True:
                    nxt = self.peek()
                    if nxt is None or nxt[0] <= indent:
                        break
                    sub.append(nxt)
                    self.i += 1
                items.append(_MiniParser(sub).parse_map(indent + 2))
            else:
                self.i += 1
                items.append(_mini_scalar(rest))
        return items

    def parse_map(self, indent: int):
        out = {}
        while True:
            cur = self.peek()
            if cur is None:
                break
            cur_indent, text = cur
            if cur_indent != indent or _is_dash(text.strip()):
                break
            split = _map_split(text)
            if not split:
                raise MiniYamlError("无法解析的行：%r" % text)
            key, rest = split
            self.i += 1
            if rest in ("|", "|-", "|+", ">", ">-", ">+"):
                out[key] = self.parse_block_scalar(indent, rest)
            elif rest == "":
                out[key] = self.parse_block(indent)
            else:
                out[key] = _mini_scalar(rest)
        return out

    def parse_block_scalar(self, indent: int, style: str):
        buf = []
        while True:
            cur = self.peek()
            if cur is None or cur[0] <= indent:
                break
            buf.append(cur[1])
            self.i += 1
        if not buf:
            return ""
        strip_indent = min(len(line) - len(line.lstrip(" ")) for line in buf)
        body = [line[strip_indent:] for line in buf]
        if style.startswith(">"):
            text = " ".join(part.strip() for part in body).strip()
        else:
            text = "\n".join(body)
        if style.endswith("-"):
            return text.rstrip("\n")
        return text + ("\n" if not text.endswith("\n") else "")


def mini_load(text: str):
    lines = []
    for raw in text.splitlines():
        if "\t" in raw[: len(raw) - len(raw.lstrip())]:
            raise MiniYamlError("缩进里出现制表符")
        stripped = raw.lstrip(" ")
        if not stripped or stripped.startswith("#"):
            continue
        lines.append((len(raw) - len(stripped), raw.rstrip()))
    parser = _MiniParser(lines)
    return parser.parse_block(-1)


def load_yaml(path: str, parser: str):
    with open(path, "r", encoding="utf-8") as fh:
        text = fh.read()
    if parser == "mini":
        return mini_load(text), "mini"
    try:
        import yaml  # type: ignore
    except ImportError:
        return mini_load(text), "mini(无 PyYAML 回退)"
    return yaml.safe_load(text), "PyYAML"


# ---------------------------------------------------------------------- 断言工具


class Report:
    def __init__(self):
        self.failures = []
        self.passes = 0

    def check(self, ok: bool, label: str, detail: str = ""):
        if ok:
            self.passes += 1
            print("  [OK]   %s" % label)
        else:
            self.failures.append((label, detail))
            print("  [FAIL] %s%s" % (label, ("  → " + detail) if detail else ""))
        return ok


def job(workflow, name):
    return (workflow.get("jobs") or {}).get(name) or {}


def steps_text(jobdef):
    """把一个 job 的全部步骤压成可搜索文本（含 run/with/if/uses）。"""
    parts = []
    for step in jobdef.get("steps") or []:
        if not isinstance(step, dict):
            continue
        for key in ("name", "uses", "if", "run"):
            value = step.get(key)
            if isinstance(value, str):
                parts.append(value)
        with_block = step.get("with")
        if isinstance(with_block, dict):
            for value in with_block.values():
                if isinstance(value, str):
                    parts.append(value)
                elif isinstance(value, list):
                    parts.extend(str(item) for item in value)
    return "\n".join(parts)


def step_using(jobdef, needle):
    for step in jobdef.get("steps") or []:
        if not isinstance(step, dict):
            continue
        uses = step.get("uses") or ""
        if isinstance(uses, str) and needle in uses:
            return step
    return {}


def run_scripts(jobdef):
    out = []
    for step in jobdef.get("steps") or []:
        if isinstance(step, dict) and isinstance(step.get("run"), str):
            out.append((step.get("name") or "run", step["run"]))
    return out


def get_key(mapping, key):
    """兼容 PyYAML 把 on/yes/no 解析成布尔键的情况。"""
    if not isinstance(mapping, dict):
        return None
    if key in mapping:
        return mapping[key]
    if key == "on" and True in mapping:
        return mapping[True]
    return None


def as_list(value):
    if value is None:
        return []
    if isinstance(value, list):
        return value
    return [value]


# ------------------------------------------------------------------ A~F 工作流断言


def check_workflow(report: Report, wf):
    print("\n[A] 触发条件与权限")
    on_block = get_key(wf, "on")
    report.check(isinstance(on_block, dict), "workflow 有 on 触发器")
    push_tags = as_list(((on_block or {}).get("push") or {}).get("tags"))
    report.check("v*" in [str(t) for t in push_tags], "push tag 触发 v*", str(push_tags))
    dispatch = ((on_block or {}).get("workflow_dispatch") or {})
    inputs = dispatch.get("inputs") or {}
    dry = inputs.get("dry_run") or {}
    report.check(str(dry.get("type")) == "boolean", "workflow_dispatch 带 dry_run 布尔入参", str(dry))
    top_perms = wf.get("permissions") or {}
    report.check(top_perms.get("contents") == "read", "顶层默认权限最小化（contents: read）", str(top_perms))

    print("\n[B] jobs 与依赖关系")
    jobs = wf.get("jobs") or {}
    for name in ("frontend", "build-packages", "release", "docker", "smoke"):
        report.check(name in jobs, "存在 job：%s" % name)
    needs = lambda n: [str(x) for x in as_list(job(wf, n).get("needs"))]
    report.check("frontend" in needs("build-packages"), "build-packages needs frontend")
    report.check("build-packages" in needs("release"), "release needs build-packages")
    report.check("docker" in needs("smoke"), "smoke needs docker")

    print("\n[C] build-packages（三平台矩阵 + 复用前端产物 + 缓存）")
    bp = job(wf, "build-packages")
    matrix = as_list((bp.get("strategy") or {}).get("matrix", {}).get("include"))
    combos = {(str(m.get("goos")), str(m.get("goarch"))) for m in matrix if isinstance(m, dict)}
    for combo in (("linux", "amd64"), ("linux", "arm64"), ("windows", "amd64")):
        report.check(combo in combos, "打包矩阵含 %s/%s" % combo, str(sorted(combos)))
    setup_go = step_using(bp, "actions/setup-go")
    report.check(bool(setup_go), "使用 actions/setup-go")
    go_with = (setup_go.get("with") or {}) if setup_go else {}
    report.check("go.sum" in str(go_with.get("cache-dependency-path", "")), "setup-go 用 go.sum 缓存", str(go_with))
    dl = step_using(bp, "actions/download-artifact")
    report.check("frontend-dist" in str((dl.get("with") or {}).get("name", "")), "下载前端 artifact（不按平台重跑 npm ci）")
    up = step_using(bp, "actions/upload-artifact")
    report.check(bool(up), "上传三平台发布包 artifact", str((up.get("with") or {}).get("name", "")))
    bp_text = steps_text(bp)
    report.check("scripts/package.sh" in bp_text, "调用共用打包脚本 scripts/package.sh")

    print("\n[D] release（仅 tag 且非 dry_run）")
    rel = job(wf, "release")
    rel_if = str(rel.get("if") or "")
    report.check("dry_run" in rel_if, "release 的 if 判断 dry_run", rel_if)
    report.check("refs/tags/v" in rel_if, "release 的 if 限定 tag", rel_if)
    report.check((rel.get("permissions") or {}).get("contents") == "write", "release 有 contents: write")
    dl = step_using(rel, "actions/download-artifact")
    report.check(bool((dl.get("with") or {}).get("merge-multiple")), "release 合并下载全部平台产物")
    rel_text = steps_text(rel)
    report.check("softprops/action-gh-release" in rel_text, "使用 softprops/action-gh-release")
    gh = step_using(rel, "softprops/action-gh-release") or {}
    gh_with = gh.get("with") or {}
    report.check(gh_with.get("generate_release_notes") in (True, "true"), "自动生成更新说明", str(gh_with))
    report.check("release-assets" in str(gh_with.get("files", "")), "Release 附上全部产物", str(gh_with.get("files")))

    print("\n[E] docker（GHCR 多架构 + tag 规则 + 缓存）")
    dk = job(wf, "docker")
    report.check((dk.get("permissions") or {}).get("packages") == "write", "docker job 有 packages: write")
    dk_text = steps_text(dk)
    report.check("docker/setup-qemu-action" in dk_text, "设置 QEMU（arm64）")
    report.check("docker/setup-buildx-action" in dk_text, "设置 Buildx")
    report.check("docker/login-action" in dk_text, "登录 GHCR")
    bp_step = step_using(dk, "docker/build-push-action")
    report.check(bool(bp_step), "使用 docker/build-push-action")
    files = [str((s.get("with") or {}).get("file", "")) for s in (dk.get("steps") or [])
             if isinstance(s, dict) and "docker/build-push-action" in str(s.get("uses", ""))]
    report.check(any("Dockerfile.backend" in f for f in files), "构建后端镜像", str(files))
    report.check(any("Dockerfile.frontend" in f for f in files), "构建前端镜像", str(files))
    # 平台矩阵既可以直接写在 build 步骤里，也可以放在 workflow 级 env（本仓库用 env.PLATFORMS）。
    platforms = " ".join(
        [str((s.get("with") or {}).get("platforms", "")) for s in (dk.get("steps") or [])
         if isinstance(s, dict) and "docker/build-push-action" in str(s.get("uses", ""))]
        + [str((wf.get("env") or {}).get("PLATFORMS", ""))])
    report.check("linux/amd64" in platforms and "linux/arm64" in platforms, "多架构镜像（amd64 + arm64）", platforms)
    tags_all = "\n".join(
        str((s.get("with") or {}).get("tags", "")) for s in (dk.get("steps") or [])
        if isinstance(s, dict) and "docker/metadata-action" in str(s.get("uses", "")))
    report.check("type=semver,pattern=v{{version}}" in tags_all, "tag 规则 vX.Y.Z")
    report.check("type=semver,pattern={{major}}.{{minor}}" in tags_all, "tag 规则 X.Y")
    report.check("type=sha,format=short,prefix=sha-" in tags_all, "tag 规则 sha-xxxxxxx")
    report.check("type=raw,value=latest" in tags_all and "startsWith(github.ref, 'refs/tags/v')" in tags_all,
                 "latest 由 type=raw 显式给且仅 tag 生效")
    report.check(tags_all.count("type=raw,value=latest") >= 2, "两个镜像都带 latest 规则")
    report.check("type=gha" in dk_text, "使用 GHA 构建缓存")
    report.check("push: ${{ env.DRY_RUN == 'false' }}" in dk_text or "env.DRY_RUN" in dk_text,
                 "dry_run 时不推送镜像")
    report.check("tr '[:upper:]' '[:lower:]'" in dk_text, "镜像名 owner 小写化（Docker 拒绝大写）")

    print("\n[F] smoke（真跑容器）")
    sm = job(wf, "smoke")
    mysql = (sm.get("services") or {}).get("mysql") or {}
    report.check(str(mysql.get("image")) == "mysql:5.7", "mysql:5.7 service 容器", str(mysql.get("image")))
    report.check(bool(mysql.get("options")) and "health-cmd" in str(mysql.get("options")), "MySQL 健康检查")
    sm_text = steps_text(sm)
    for needle, label in [
        ("/install/api/status", "首启调用安装状态接口"),
        ('"unconfigured"', "断言未安装态（安装向导未完成）"),
        ("${BASE}/install/api/${step}", "安装向导统一走 /install/api/<step>"),
        ("wizard database/test", "向导第 2 步：数据库连通性"),
        ("wizard initialize", "向导第 3 步：初始化建表"),
        ('"username":"smokeadmin"', "向导第 4 步：管理员账号"),
        ('"name":"Smoke Site"', "向导第 5 步：站点信息"),
        ("wizard complete", "向导第 6 步：完成安装"),
        ('.data.db == "up"', "断言就绪态（/api/v1/health → db up）"),
        ('<div id="root"', "前端首页含 SPA 挂载点"),
        ('${WEB}/api/v1/health', "经前端反代打后端健康检查"),
        ("State.Health.Status", "读取 docker healthcheck 状态"),
        ("dump_logs", "失败时 dump 容器日志"),
        ("docker rm -f", "失败/成功后清理容器"),
    ]:
        report.check(needle in sm_text, "冒烟断言：%s" % label, needle)
    report.check("health=${state}" in sm_text or "healthy" in sm_text, "等待并断言 healthy")
    report.check("docker build" in sm_text, "冒烟自建镜像（不依赖 registry 可拉取性）")

    print("\n[G] 内嵌 run 脚本的 shell 语法（sh -n）")
    for job_name in ("frontend", "build-packages", "release", "docker", "smoke"):
        for label, script in run_scripts(job(wf, job_name)):
            ok, detail = sh_syntax_check(script)
            report.check(ok, "sh -n：%s / %s" % (job_name, label), detail)


def sh_syntax_check(script: str):
    with tempfile.NamedTemporaryFile("w", suffix=".sh", delete=False, encoding="utf-8", newline="\n") as fh:
        fh.write(script)
        path = fh.name
    try:
        proc = subprocess.run(["sh", "-n", path], capture_output=True, text=True)
        return proc.returncode == 0, (proc.stderr or "").strip()[:300]
    except FileNotFoundError:
        return False, "未找到 sh 命令"
    finally:
        os.unlink(path)


# ------------------------------------------------------------- H/I 交付文件形式审查


def check_dockerfile(report: Report, path: str, runtime_image: str, label: str):
    with open(path, "r", encoding="utf-8") as fh:
        text = fh.read()
    lines = [line for line in text.splitlines() if not line.strip().startswith("#")]
    froms = [i for i, line in enumerate(lines) if re.match(r"^\s*FROM\s+\S", line, re.I)]
    copies = [i for i, line in enumerate(lines) if re.match(r"^\s*COPY\s", line, re.I)]
    report.check(len(froms) >= 2, "%s 多阶段构建" % label, "FROM 数：%d" % len(froms))
    report.check(any(runtime_image in lines[i] for i in froms), "%s 运行时基础镜像含 %s" % (label, runtime_image))
    health = [line for line in lines if line.strip().upper().startswith("HEALTHCHECK")]
    report.check(bool(health), "%s 定义 HEALTHCHECK" % label)
    if health:
        blob = " ".join(health)
        report.check("--interval" in blob and "--timeout" in blob and "--start-period" in blob and "--retries" in blob,
                     "%s HEALTHCHECK 参数完整" % label, blob[:200])
    # ARG TARGETARCH 必须在 FROM 之后、COPY 之前（放 FROM 前会取不到 buildx 注入的值）
    arg_idx = next((i for i, line in enumerate(lines) if re.match(r"^\s*ARG\s+TARGETARCH\b", line)), None)
    report.check(arg_idx is not None, "%s 声明 ARG TARGETARCH" % label)
    if arg_idx is not None and froms and copies:
        report.check(froms[0] < arg_idx < copies[0],
                     "%s ARG TARGETARCH 位于 FROM 之后、首个 COPY 之前" % label,
                     "FROM@%s ARG@%s COPY@%s" % (froms[0], arg_idx, copies[0]))
    report.check(not re.search(r"^\s*ARG\s+TARGET", text[: text.find("\nFROM")] if "\nFROM" in text else "", re.M),
                 "%s 未在 FROM 之前声明平台 ARG" % label)


def check_compose(report: Report, compose):
    services = (compose or {}).get("services") or {}
    for name in ("mysql", "backend", "frontend"):
        report.check(name in services, "compose 含服务 %s" % name)
    mysql = services.get("mysql") or {}
    report.check(str(mysql.get("image")) == "mysql:5.7", "compose mysql 镜像 5.7", str(mysql.get("image")))
    report.check(bool(mysql.get("healthcheck")), "compose mysql 有健康检查")
    report.check(any("mysql-data" in str(v) for v in as_list(mysql.get("volumes"))), "compose mysql 用命名卷")
    backend = services.get("backend") or {}
    report.check(any("/app/data" in str(v) for v in as_list(backend.get("volumes"))), "compose backend 数据卷挂 /app/data")
    report.check("service_healthy" in str((backend.get("depends_on") or {}).get("mysql", {})),
                 "compose backend 依赖 mysql 健康")
    frontend = services.get("frontend") or {}
    report.check("service_healthy" in str((frontend.get("depends_on") or {}).get("backend", {})),
                 "compose frontend 依赖 backend 健康")
    report.check(any("BACKEND_UPSTREAM" in str(k) for k in (frontend.get("environment") or {})),
                 "compose frontend 注入 BACKEND_UPSTREAM")
    report.check(any(":80" in str(p) for p in as_list(frontend.get("ports"))), "compose frontend 对外发布 80")
    report.check(len((compose or {}).get("volumes") or {}) >= 2, "compose 声明命名卷")


def check_shell_files(report: Report):
    print("\n[G2] 交付 shell 脚本语法（sh -n）")
    for path in (os.path.join(ROOT, "scripts", "package.sh"), ENTRYPOINT):
        with open(path, "r", encoding="utf-8") as fh:
            ok, detail = sh_syntax_check(fh.read())
        report.check(ok, "sh -n：%s" % os.path.relpath(path, ROOT).replace("\\", "/"), detail)


def check_static_files(report: Report):
    with open(NGINX_TEMPLATE, "r", encoding="utf-8") as fh:
        tpl = fh.read()
    print("\n[H/I] 交付文件形式审查")
    report.check("BACKEND_UPSTREAM" in tpl, "nginx 模板用 BACKEND_UPSTREAM 变量")
    report.check("/api/" in tpl and "/install" in tpl, "nginx 模板反代 /api/ 与 /install")
    report.check("${" in tpl, "nginx 模板走 envsubst（${...} 形式）")
    report.check("try_files" in tpl and "index.html" in tpl, "nginx 模板含 SPA 回退")
    report.check(not re.search(r"\$\{[A-Za-z_]+\}", tpl.replace("${BACKEND_UPSTREAM}", "")),
                 "nginx 模板未误用其它 ${}（envsubst 会替换掉未定义变量以外的 nginx 变量：此处只允许 BACKEND_UPSTREAM）")
    with open(ENTRYPOINT, "r", encoding="utf-8") as fh:
        entry = fh.read()
    report.check("config.yaml" in entry and "0.0.0.0:8080" in entry, "入口脚本生成 0.0.0.0:8080 种子配置")
    report.check("exec /app/lyidc-server" in entry, "入口脚本 exec 后端二进制（PID 1 收信号）")
    compose_env = os.path.join(ROOT, "deploy", ".env.example")
    with open(compose_env, "r", encoding="utf-8") as fh:
        env_example = fh.read()
    report.check("MYSQL_ROOT_PASSWORD" in env_example, ".env.example 含 MYSQL_ROOT_PASSWORD")


def main() -> int:
    parser_arg = "auto"
    args = sys.argv[1:]
    if "--parser" in args:
        parser_arg = args[args.index("--parser") + 1]
    if parser_arg not in ("auto", "mini", "pyyaml"):
        print("未知 --parser 取值：%s" % parser_arg)
        return 2

    report = Report()
    print("Lyidc_OEM 交付资产自检")
    print("仓库根：%s" % ROOT)

    try:
        wf, used = load_yaml(WORKFLOW, parser_arg)
    except Exception as exc:  # noqa: BLE001 - 解析失败必须显式报错而不是崩栈
        print("解析 %s 失败：%s" % (WORKFLOW, exc))
        return 1
    print("YAML 解析器：%s" % used)

    check_workflow(report, wf or {})

    try:
        compose, _ = load_yaml(COMPOSE, parser_arg)
    except Exception as exc:  # noqa: BLE001
        print("解析 %s 失败：%s" % (COMPOSE, exc))
        return 1

    check_shell_files(report)
    check_static_files(report)
    check_dockerfile(report, DOCKERFILE_BACKEND, "alpine", "Dockerfile.backend")
    check_dockerfile(report, DOCKERFILE_FRONTEND, "nginx", "Dockerfile.frontend")
    check_compose(report, compose or {})

    print("\n=== 结果：通过 %d 项，失败 %d 项 ===" % (report.passes, len(report.failures)))
    for label, detail in report.failures:
        print("  FAIL %s %s" % (label, ("→ " + detail) if detail else ""))
    return 1 if report.failures else 0


if __name__ == "__main__":
    sys.exit(main())
