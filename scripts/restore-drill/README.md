# Restore drill (MinIO)

Boots ssh-farm against a local MinIO container using the exact
Dockerfile/entrypoint the fleet runs against real S3, kills the container
without a graceful flush, deletes its volume, and confirms a fresh
container restores from the bucket and starts serving.

```sh
./run.sh
```

Requires Docker with Compose v2. This is framework/02's own minimal
integration check (its acceptance criterion: "Fresh container with empty
volume + populated bucket restores and serves the restored farms").

The MinIO server and `mc` client are built by the test-only `Dockerfile` in
this directory from the immutable source commits for the previously used
releases. This avoids the unavailable `minio/minio` and `minio/mc` Docker Hub
images. No Docker login or AWS credentials are needed; the first run needs
network access to fetch the public Go/Alpine base images and Go modules.
Subsequent runs reuse Docker's build cache.

The nightly workflow runs this disposable stack on a GitHub-hosted runner,
using synthetic saves and a local MinIO bucket. It does not access EC2 or
the production S3 bucket. The scripts delete their test containers and
volumes, so run them on a development/CI Docker daemon. They are not a
substitute for restoring a real production backup into scratch storage.

[tests/02](../../docs/tests/02-leaderboard-moderation-and-durability-tests.md)
extends this same compose stack with the full drill, meant to run on a
schedule as a monitoring tool, not just a one-off test:

```sh
./kill-drill.sh            # docker kill + integrity_check + decode-every-save + measured RPO
./restore-to-scratch.sh    # litestream restore straight from the S3 replica, no game container involved
./no-credentials-check.sh  # dev mode (no LITESTREAM_REPLICA_URL) never touches S3/MinIO at all
```

`kill-drill.sh` and `restore-to-scratch.sh` both play a real scripted SSH
session to create genuine save data before killing the container, so they
need an `ssh`/`ssh-keygen` client on the host as well as Docker.
`kill-drill.sh` additionally proves the container's own restore-on-boot path
(`entrypoint.sh`) works; `restore-to-scratch.sh` isolates the S3 replica
itself from that, using `cmd/restore-check` (built into the same image) for
the integrity/decode verification either way.
