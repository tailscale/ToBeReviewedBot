# Build the Docker image and push to fly.io's container registry
# Change $REGISTRY from ts-tbrbot to your fly.io app name and update
# the [build] image in fly.toml to match.

REGISTRY=registry.fly.io/ts-tbrbot:latest

all: build

build:
	docker build -t ${REGISTRY} .

push:
	docker push ${REGISTRY}
