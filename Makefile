.PHONY: dev backend frontend build run clean

# 开发：后端 :5260 + 前端 Vite :5173（proxy /api）
dev:
	$(MAKE) -j2 backend frontend

backend:
	go run .

frontend:
	cd frontend && npm run dev

# 发布：前端产物嵌入单二进制
build:
	cd frontend && npm run build
	go build -tags release -o ezharness.exe .

run: build
	./ezharness.exe

clean:
	rm -f ezharness.exe
