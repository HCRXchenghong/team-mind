#!/usr/bin/env bash
# Prepare the Docker image used by TeamMind's macOS code sandbox.
# The sandbox runs Python 3 and POSIX sh inside a network-less, read-only,
# non-root container. Linux hosts keep using bubblewrap and do not need this.
set -euo pipefail

IMAGE="${TEAMMIND_SANDBOX_IMAGE:-teammind-sandbox-python:local}"
BASE="${TEAMMIND_SANDBOX_BASE_IMAGE:-docker.m.daocloud.io/library/python:3.12-alpine}"

if ! command -v docker >/dev/null 2>&1; then
  echo "docker 未安装：macOS 沙箱需要 Docker Desktop" >&2
  exit 1
fi
if ! docker info >/dev/null 2>&1; then
  echo "Docker 守护进程未运行，请先启动 Docker Desktop" >&2
  exit 1
fi
if docker image inspect "$IMAGE" >/dev/null 2>&1; then
  echo "沙箱镜像已存在：$IMAGE"
  exit 0
fi
echo "拉取基础镜像：$BASE"
if ! docker pull "$BASE"; then
  FALLBACK="${TEAMMIND_SANDBOX_FALLBACK_IMAGE:-python:3.12-alpine}"
  echo "镜像源不可用，改用 $FALLBACK" >&2
  docker pull "$FALLBACK"
  docker tag "$FALLBACK" "$IMAGE"
else
  docker tag "$BASE" "$IMAGE"
fi
echo "沙箱镜像就绪：$IMAGE"
