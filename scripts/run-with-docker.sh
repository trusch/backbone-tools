#!/bin/bash

docker run -d --rm --name postgres -e POSTGRES_PASSWORD=postgres postgres
docker run -d --rm --name backbone-tools-server --link postgres -p3001:3001 trusch/backbone-tools:latest "--db=postgres://postgres@postgres:5432?sslmode=disable&password=postgres"

exit $?
