# Sanity checks

`make test-sanity` runs the upstream CSI sanity suite against a live driver.
It needs Engine credentials and a storage server and creates real volumes.

`make test-sanity-recovery` runs deterministic controller regression tests,
without Engine credentials or NFS. These supplement the upstream suite:

- cancel a copy after it writes partial data, then retry with a new manager;
- recover failed copies while preserving the snapshot identity during cleanup;
- leave completed restores and unmarked workload volumes untouched;
- recognize older completion metadata and reject invalid operation metadata;
- reject restore capacity limits smaller than the snapshot before allocation;
- use an independent, bounded cleanup context after cancellation.

It also checks that the deployment grants `update` on `volumesnapshots` to
the controller service account for the provisioner's restore-source finalizer.

The filesystem tests replace NFS mounts and the copier executable, but exercise
the real restore metadata, cleanup, and subprocess cancellation code. They do
not simulate a host power failure or an unavailable NFS server.

`make test-snapshot-runtime` builds and tests the actual Linux runtime image.
It checks GNU cp's ACL, xattr, hard-link and metadata preservation on the
regular container filesystem (`/var/tmp`), since Docker Desktop's tmpfs may
not support POSIX ACLs. ACL assertions remain mandatory. A separate test
copies a 64 MiB sparse file through snapshot and restore on a 32 MiB tmpfs,
so expanding its holes cannot pass unnoticed. Another test deliberately
exhausts that filesystem during snapshot creation and restore. Repeated
disk-full failures must leave each operation incomplete and recoverable;
after space is released, a fresh manager must copy the original bytes successfully.
The source is outside the constrained filesystem and remains unchanged.
Docker is required. The same runtime-image tests run in CI.
