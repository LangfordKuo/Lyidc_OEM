#!/bin/sh
# 后端容器入口：确保配置文件存在（首次启动生成最小配置），再执行后端二进制。
#
# 为什么需要这一步：
#   1. 后端容器必须监听 0.0.0.0:8080，而内置缺省值是 127.0.0.1:8080（本机开发用），
#      在容器里会导致端口映射打不进来；
#   2. 配置文件是安装向导写进数据卷的（契约 13.4），首启时还不存在。
# 因此首启先落一份**最小种子配置**：只写 server.addr，不含 database.dsn——
# 安装状态机据此仍判定为「未找到配置文件或未提供 database.dsn」（契约 13.1 场景 1），
# 向导照常从第 1 步开始；完成后把 database.dsn 与 jwt.secret 合并写回同一文件，
# 既有键（server.addr 等）保持不变，容器重启后继续生效。
set -eu

CONFIG="${LYIDC_CONFIG:-/app/data/config.yaml}"

case "$CONFIG" in
  /app/data/*) : ;;
  *)
    echo "[entrypoint] 警告：LYIDC_CONFIG=${CONFIG} 不在数据卷 /app/data 内，容器重建后会丢失；" >&2
    echo "[entrypoint]       建议把该路径所在目录也挂成卷（见 docs/deploy.md）。" >&2
    ;;
esac

if [ -f "$CONFIG" ]; then
  echo "[entrypoint] 使用既有配置：${CONFIG}"
else
  mkdir -p "$(dirname "$CONFIG")" 2>/dev/null || true
  if cat > "$CONFIG" <<'YAML'
# Lyidc_OEM 容器首次启动自动生成的最小配置（由 deploy/docker/backend-entrypoint.sh 写入）。
# 安装向导完成后会把 database.dsn 与 jwt.secret 合并写入本文件，其余键保持不变。
server:
  addr: "0.0.0.0:8080"
  mode: "release"
YAML
  then
    echo "[entrypoint] 已生成容器默认配置：${CONFIG}（浏览器访问 /install 开始安装）"
  else
    echo "[entrypoint] 无法写入 ${CONFIG}（数据卷权限不足？）——将以内置缺省配置启动。" >&2
    echo "[entrypoint] 若安装向导第 1 步的「配置写入点可写」检查失败，请按 docs/deploy.md 处理卷权限。" >&2
  fi
fi

exec /app/lyidc-server "$@"
