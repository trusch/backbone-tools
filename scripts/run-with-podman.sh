#!/bin/bash

podman pod create --name backbone-tools -p 3001:3001
podman run --pod backbone-tools -d --name postgres -e POSTGRES_PASSWORD=postgres postgres
podman run --pod backbone-tools -d --name backbone-tools-server trusch/backbone-tools:latest

exit $?
