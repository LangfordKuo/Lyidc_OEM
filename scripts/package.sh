#!/usr/bin/env bash
# Lyidc_OEM 发布包打包脚本（CI 与本地共用）。
#
# 用途：把一个平台的后端二进制 + 前端构建产物 + 部署文件打成一个归档，
# 供 GitHub Release 下载后**解包即用**（详见 docs/deploy.md 的「本地二进制」一节）。
#
# 产物布局（解包后的目录树）：
#   lyidc-oem-<版本>-<os>-<arch>/
#     lyidc-server[.exe]        后端可执行文件（静态编译，无外部依赖）
#     lyidc-migrate[.exe]       数据库迁移命令（升级含新迁移的版本时手动执行一次）
#     dist/                     前端静态产物（用任意静态服务器托管，反代 /api/v1 与 /install）
#     config.example.yaml       后端配置示例（走安装向导的话可以不写配置文件）
#     deploy/docker-compose.yml 容器编排（二选一：容器部署更省事）
#     deploy/.env.example       compose 环境变量示例
#     docs/deploy.md            部署文档（含三种部署方式与故障排查）
#     README.md                 项目简介
#     VERSION                   本包对应的版本号/提交
#
# 用法：
#   scripts/package.sh [选项]
#     -v, --version VER    版本号（缺省：git describe --tags --always，兜底 dev）
#     -p, --platforms LIST 平台列表，空格分隔（缺省 "linux/amd64 linux/arm64 windows/amd64"）
#     -d, --dist DIR       前端构建产物目录（缺省 frontend/dist，须先 npm run build）
#     -o, --outdir DIR     归档输出目录（缺省 build/packages）
#     -s, --source DIR     后端源码目录（缺省 backend）
#     -h, --help           显示帮助
#
# 例（本地打全部三平台，前端产物复用 frontend/dist）：
#   scripts/package.sh -v v1.0.0
set -eu

script_dir=$(cd "$(dirname "$0")" && pwd)
repo_root=$(cd "$script_dir/.." && pwd)

version=""
platforms="linux/amd64 linux/arm64 windows/amd64"
dist_dir=""
outdir=""
source_dir=""

usage() {
  # 打印文件头部注释块（第 2 行起连续的 # 行）
  awk 'NR > 1 && /^#/ { sub(/^# ?/, ""); print; next } NR > 1 { exit }' "$0"
}

die() {
  echo "错误：$*" >&2
  exit 1
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    -v|--version)   [ "$#" -ge 2 ] || die "$1 需要参数"; version=$2; shift 2 ;;
    -p|--platforms) [ "$#" -ge 2 ] || die "$1 需要参数"; platforms=$2; shift 2 ;;
    -d|--dist)      [ "$#" -ge 2 ] || die "$1 需要参数"; dist_dir=$2; shift 2 ;;
    -o|--outdir)    [ "$#" -ge 2 ] || die "$1 需要参数"; outdir=$2; shift 2 ;;
    -s|--source)    [ "$#" -ge 2 ] || die "$1 需要参数"; source_dir=$2; shift 2 ;;
    -h|--help)      usage; exit 0 ;;
    *)              die "未知参数：$1（用 -h 查看用法）" ;;
  esac
done

# 相对路径统一按仓库根解析，脚本在任意目录下调用都成立。
abs_path() {
  case "$1" in
    /*|[A-Za-z]:*) printf '%s' "$1" ;;
    *) printf '%s/%s' "$repo_root" "$1" ;;
  esac
}

# 把 MSYS/Cygwin 风格路径（/d/...）转成原生 Windows 路径（D:\...）。
# 必要性：go.exe 是原生程序，收到 "/d/Project/..." 会当成当前盘上的相对路径，
# 产物会落到 D:\d\Project\...（静默成功、目标位置什么都没有）。Linux/CI 上是 no-op。
native_path() {
  if command -v cygpath >/dev/null 2>&1; then
    cygpath -w "$1"
  else
    printf '%s' "$1"
  fi
}

[ -n "$dist_dir" ]   || dist_dir="frontend/dist"
[ -n "$outdir" ]     || outdir="build/packages"
[ -n "$source_dir" ] || source_dir="backend"

dist_dir=$(abs_path "$dist_dir")
outdir=$(abs_path "$outdir")
source_dir=$(abs_path "$source_dir")

if [ -z "$version" ]; then
  if command -v git >/dev/null 2>&1 && git -C "$repo_root" rev-parse --git-dir >/dev/null 2>&1; then
    version=$(git -C "$repo_root" describe --tags --always --dirty 2>/dev/null || echo dev)
  else
    version="dev"
  fi
fi
# 版本号进归档文件名：只保留安全字符，避免路径穿越与空格。
safe_version=$(printf '%s' "$version" | tr -c 'A-Za-z0-9._-' '-' | sed 's/-\{2,\}/-/g; s/-$//')
[ -n "$safe_version" ] || safe_version="dev"

[ -d "$source_dir" ] || die "后端源码目录不存在：$source_dir"
[ -d "$dist_dir" ] || die "前端构建产物目录不存在：$dist_dir（请先在 frontend/ 执行 npm run build，或用 -d 指定）"
[ -f "$dist_dir/index.html" ] || die "前端产物不完整：$dist_dir/index.html 不存在（请重新 npm run build）"
command -v go >/dev/null 2>&1 || die "未找到 go 命令（后端交叉编译需要 Go 工具链）"

mkdir -p "$outdir"
stage_root="$outdir/.stage"
rm -rf "$stage_root"
mkdir -p "$stage_root"

# 归档时按 SHA256 记录摘要：产物下载后可用它校验完整性。
write_sha256() {
  file=$1
  base=$(basename "$file")
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$outdir" && sha256sum "$base" > "$base.sha256")
  elif command -v shasum >/dev/null 2>&1; then
    (cd "$outdir" && shasum -a 256 "$base" > "$base.sha256")
  else
    echo "警告：未找到 sha256sum/shasum，跳过摘要：$base" >&2
    return 0
  fi
}

# 选择 zip 工具链并回显标识：zip（CI 常见）> bsdtar（Windows 自带 tar.exe，
# GNU tar 不支持写 zip，故用 --version 里的 bsdtar 字样甄别）> PowerShell Compress-Archive。
zip_tool() {
  if command -v zip >/dev/null 2>&1; then
    printf 'zip'
    return 0
  fi
  for bsdtar in "${SYSTEMROOT:-/c/Windows}/System32/tar.exe" /c/Windows/System32/tar.exe; do
    if [ -x "$bsdtar" ] && "$bsdtar" --version 2>/dev/null | grep -qi bsdtar; then
      printf '%s' "$bsdtar"
      return 0
    fi
  done
  if command -v powershell.exe >/dev/null 2>&1; then
    printf 'powershell'
    return 0
  fi
  printf ''
}

make_zip() {
  src_dir=$1
  base=$2
  archive=$3
  tool=$(zip_tool)
  # bsdtar 与 PowerShell 都是原生 Windows 程序：归档路径必须先转成原生格式（见 native_path）。
  native_archive=$(native_path "$archive")
  case "$tool" in
    zip)      (cd "$src_dir" && zip -qr "$archive" "$base") ;;
    powershell)
      win_src=$(cd "$src_dir" && pwd -W 2>/dev/null || printf '%s' "$src_dir")
      win_out=$(cd "$(dirname "$archive")" && pwd -W 2>/dev/null || printf '%s' "$(dirname "$archive")")
      powershell.exe -NoProfile -Command \
        "Compress-Archive -Path '${win_src}/${base}' -DestinationPath '${win_out}/$(basename "$archive")' -Force" \
        >/dev/null || die "Compress-Archive 打包失败：$archive" ;;
    "")       die "未找到 zip / bsdtar / PowerShell，无法生成 $archive" ;;
    *)        (cd "$src_dir" && "$tool" -a -c -f "$native_archive" "$base") ;;
  esac
}

list_zip() {
  archive=$1
  tool=$(zip_tool)
  case "$tool" in
    zip) command -v unzip >/dev/null 2>&1 && unzip -l "$archive" || tar -tf "$archive" ;;
    "")  tar -tf "$archive" 2>/dev/null || true ;;
    *)   "$tool" -tf "$(native_path "$archive")" ;;
  esac
}

echo "== Lyidc_OEM 打包 =="
echo "版本：$version（文件名用：$safe_version）"
echo "平台：$platforms"
echo "前端产物：$dist_dir"
echo "输出目录：$outdir"
echo

for platform in $platforms; do
  goos=${platform%%/*}
  goarch=${platform##*/}
  case "$goos" in
    linux|darwin|windows) ;;
    *) die "不支持的 GOOS：$goos（仅支持 linux/darwin/windows）" ;;
  esac
  case "$goarch" in
    amd64|arm64) ;;
    *) die "不支持的 GOARCH：$goarch（仅支持 amd64/arm64）" ;;
  esac

  bin_name="lyidc-server"
  migrate_name="lyidc-migrate"
  if [ "$goos" = "windows" ]; then
    bin_name="lyidc-server.exe"
    migrate_name="lyidc-migrate.exe"
  fi

  pkg_name="lyidc-oem-${safe_version}-${goos}-${goarch}"
  stage="$stage_root/$pkg_name"
  rm -rf "$stage"
  mkdir -p "$stage"

  echo "-- [$goos/$goarch] 交叉编译后端（CGO_ENABLED=0，静态链接）"
  (
    cd "$source_dir"
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
      go build -trimpath -ldflags "-s -w" -o "$(native_path "$stage/$bin_name")" ./cmd/server
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
      go build -trimpath -ldflags "-s -w" -o "$(native_path "$stage/$migrate_name")" ./cmd/migrate
  )
  # 静默失败比编译失败更难查：产物必须存在且非空（Windows 上路径格式写错时 go 会「成功」但什么都不产出）。
  [ -s "$stage/$bin_name" ] || die "构建产物缺失或为空：$stage/$bin_name（Windows 下请确认路径已转为原生格式）"
  [ -s "$stage/$migrate_name" ] || die "构建产物缺失或为空：$stage/$migrate_name"

  echo "-- [$goos/$goarch] 组装发布目录"
  mkdir -p "$stage/dist" "$stage/deploy" "$stage/docs"
  cp -R "$dist_dir/." "$stage/dist/"
  cp "$source_dir/config.example.yaml" "$stage/config.example.yaml"
  cp "$repo_root/README.md" "$stage/README.md"
  cp "$repo_root/docs/deploy.md" "$stage/docs/deploy.md"
  cp "$repo_root/deploy/docker-compose.yml" "$stage/deploy/docker-compose.yml"
  cp "$repo_root/deploy/.env.example" "$stage/deploy/.env.example"
  cat > "$stage/VERSION" <<EOF
version: ${version}
platform: ${goos}/${goarch}
built_at: $(date -u '+%Y-%m-%dT%H:%M:%SZ')
EOF
  chmod 0755 "$stage/$bin_name" "$stage/$migrate_name" 2>/dev/null || true

  if [ "$goos" = "windows" ]; then
    archive="$outdir/$pkg_name.zip"
    rm -f "$archive"
    make_zip "$stage_root" "$pkg_name" "$archive"
  else
    archive="$outdir/$pkg_name.tar.gz"
    rm -f "$archive"
    tar -czf "$archive" -C "$stage_root" "$pkg_name"
  fi
  [ -s "$archive" ] || die "归档生成失败：$archive"

  write_sha256 "$archive"
  rm -rf "$stage"

  echo "-- [$goos/$goarch] 归档：$(basename "$archive")"
  if [ "$goos" = "windows" ]; then
    list_zip "$archive"
  else
    tar -tzf "$archive"
  fi
  echo
done

rmdir "$stage_root" 2>/dev/null || true

echo "== 完成 =="
ls -l "$outdir"
