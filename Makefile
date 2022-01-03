.PHONY: docker
docker: docker-build docker-push

.PHONY: docker-build
docker-build:
	docker build -t ghcr.io/lindluni/org-runner-manager:latest .

.PHONY: docker-push
docker-push:
	docker push ghcr.io/lindluni/org-runner-manager:latest
