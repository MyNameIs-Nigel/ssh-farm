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
volume + populated bucket restores and serves the restored farms"). The
full drill — `docker kill` + `PRAGMA integrity_check` + decode-every-blob +
measured RPO, run on a schedule — is
[tests/02](../../docs/tests/02-leaderboard-moderation-and-durability-tests.md)'s
deliverable and will extend this same compose stack.
