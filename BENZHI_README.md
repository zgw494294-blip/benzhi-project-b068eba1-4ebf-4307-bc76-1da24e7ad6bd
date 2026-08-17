# BENZHI_README

## 项目说明
- 项目：benzhi-project-b068eba1-4ebf-4307-bc76-1da24e7ad6bd
- 项目用途：SeedPool is a standard-library Go HTTP JSON service for deterministic seed-packet allocation. Its default command now performs a bounded startup check, persistent serving is available with -serve, and all required validations pass.
- Go 工具链：`golang:1.22.0`
- 前端工具链：无

## 标准构建、运行和测试命令
进入容器后执行：
cd '/app' && GOTOOLCHAIN=local go build ./...
cd /app && GOTOOLCHAIN=local go run ./cmd/seedpool
cd '/app' && GOTOOLCHAIN=local go test ./...

## Docker 构建和进入容器
chmod +x build_benzhi_docker.sh
./build_benzhi_docker.sh benzhi-project-b068eba1-4ebf-4307-bc76-1da24e7ad6bd-amd64 linux/amd64
./build_benzhi_docker.sh benzhi-project-b068eba1-4ebf-4307-bc76-1da24e7ad6bd-arm64 linux/arm64
docker run -it benzhi-project-b068eba1-4ebf-4307-bc76-1da24e7ad6bd-amd64:latest

## 题目验证命令
1. 预期退出码 0：`go test ./...`
2. 预期退出码 0：`go run ./cmd/seedpool -smoke`
