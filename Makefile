PROJECT_NAME=github_repo_sync
.PHONY: build clean

build:
	go build -o $(PROJECT_NAME) ./cmd/main/main.go

build_static:
	CGO_ENABLED=0 go build -a -ldflags '-extldflags "-static"' -o $(PROJECT_NAME)_static ./cmd/main/main.go

clean:
	rm -f $(PROJECT_NAME)

run: build
	./$(PROJECT_NAME)

service_install:
	./install_service.sh

service_restart:
	sudo systemctl daemon-reload
	sudo systemctl restart

service_logs:
	sudo journalctl -u $(PROJECT_NAME)