backbone-tools
==============

backbone-tools is a small service on top of postgresql providing the following functionality:

* jobs
* cronjobs
* locks
* events

It does so by exposing a gRPC APIs for the named topics.

# Test Setup

```bash
export CONTAINER_ENGINE=podman # valid values are 'podman' and 'docker'
export BACKBONECTL_DISABLE_TLS=true

curl https://raw.githubusercontent.com/trusch/backbone-tools/master/scripts/run-with-${CONTAINER_ENGINE}.sh | sh -
alias bctl="${CONTAINER_ENGINE} exec -it backbone-tools-server backbonectl"
```
# Job Management

```bash
bctl jobs create --queue q1 --spec '{"foo":"bar"}'
bctl jobs list --queue q1
{
  "id": "2ad4a365-0bc7-4c4b-93ed-defbc52fcb16",
  "queue": "q1",
  "spec": "eyJmb28iOiJiYXIifQ==",
  "createdAt": "2020-03-12T09:29:41.287106Z"
}
bctl jobs heartbeat --id 2ad4a365-0bc7-4c4b-93ed-defbc52fcb16 --status '{"progress": "50%"}'
bctl jobs heartbeat --id 2ad4a365-0bc7-4c4b-93ed-defbc52fcb16 --status '{"progress": "100%"}' --finished
bctl jobs list
{
  "id": "2ad4a365-0bc7-4c4b-93ed-defbc52fcb16",
  "queue": "q1",
  "spec": "eyJmb28iOiJiYXIifQ==",
  "state": "eyJwcm9ncmVzcyI6ICIxMDAlIn0=",
  "createdAt": "2020-03-12T09:29:41.287106Z",
  "updatedAt": "2020-03-12T09:31:22.129930Z",
  "finishedAt": "2020-03-12T09:31:22.129930Z"
}
```

# CronJob Management

```bash
bctl cronjobs create --queue q1 --spec '{"foo":"bar"}' --cron "@every 5m" --name cron-job-1
{
  "id": "db49e939-a04a-477b-8cbf-4349716fa884",
  "name": "cron-job-1"
  "queue": "q1",
  "spec": "eyJmb28iOiJiYXIifQ==",
  "cron": "@every 5m",
  "createdAt": "2020-03-12T09:33:06.677779248Z",
  "nextRunAt": "2020-03-12T09:33:06.677779248Z"
}
bctl jobs list
[...]
{
  "id": "cb1e9800-de9d-4fd0-b7ab-010e68ceea64",
  "queue": "q1",
  "spec": "eyJmb28iOiJiYXIifQ==",
  "labels": {
    "@system/cronjob-id": "db49e939-a04a-477b-8cbf-4349716fa884",
    "@system/cronjob-name": "cron-job-1"
  },
  "createdAt": "2020-03-12T09:33:06.695744Z"
}
```
