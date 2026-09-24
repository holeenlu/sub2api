# Release matrix

The release workflow builds the frontend once, then runs each configured Go target on its own Linux runner. `CGO_ENABLED=0` permits cross-compilation. The target matrix is read from `.goreleaser.yaml`, including its exclusions; simple releases select Linux amd64 only.

`automatic-release.yml` uses the same target matrix, GoReleaser build configuration and runtime-image packaging. Its `--channel` option selects the archive prefix and injects the application version, upstream version and update channel; the packaged executable remains `sub2api` for compatibility with the Docker entrypoint and updater. Three- and four-part versions are supported. The automatic publisher uploads the verified archives to a reserved draft and adds the Compose update manifest. It promotes `latest` only after the release is published, and can retry that promotion without rebuilding an already-published commit.

Each build uses GoReleaser OSS in snapshot mode with the selected release version and one target. Archive naming, bundled files, Go flags and release templates remain in the existing GoReleaser configurations. Every archive is accompanied by its source commit, target, version and SHA256. The publishing job verifies the complete matrix before building images or publishing. The original `release.yml` publisher uses GoReleaser's `extra_files` support with builds disabled; the automatic channel publisher uses GitHub CLI to upload to the reserved draft, including four-part versions. No Pro license is needed.

Go caches are isolated by target and refreshed on each source commit, with fallback to the preceding target cache. Save uses the original restore key, even if a build hook changes `go.sum`. Matrix jobs upload uniquely named artifacts. The publishing job extracts only the regular Linux binary from each verified archive and restores its executable permission before constructing Docker contexts. QEMU remains limited to runtime-image instructions. DockerHub images are omitted when its credentials are absent; GHCR is always retained. Simple mode still publishes only the amd64 GHCR image and the simple release description.

All build jobs use the commit resolved by `prepare`, including a manual release's selected tag. In `release.yml`, helper scripts come from the workflow revision and are passed as a run-local artifact, so older application tags do not need to contain the new scripts. Automatic releases use the helpers in their pinned source commit. Both workflows serialize their release runs to prevent simultaneous updates to moving image tags.

## Validate without publication

In the upstream repository, from a branch containing `release.yml` (its repository guard intentionally skips forks):

```bash
gh workflow run release.yml --ref <branch> \
  -f tag=<branch> -f dry_run=true -f simple_release=false
```

A dry run builds all selected archives and both runtime images, verifies artifact provenance and produces the final checksum file. It exports images locally as OCI archives instead of pushing them. It skips registry logins, GitHub Release publication, DockerHub description updates, Telegram notifications and VERSION synchronization. Test the simple path separately with `simple_release=true`.

Dry-run artifacts are available in the Actions run, including `release-dry-run-report`. Compare job start/end times, GoReleaser's build duration and cache restore results. Do not present an initial cold-cache run as a warmed-cache benchmark; publishing network time is not measured by dry runs. For this fork's automatic release entry point and retry procedure, see [AUTOMATIC_RELEASE.md](../../deploy/AUTOMATIC_RELEASE.md).

Helper checks:

```bash
python -m pip install -r .github/release-tools/requirements-release.txt
python -m unittest discover -s .github/release-tools -p 'test_release_matrix.py'
bash -n .github/release-tools/release-images.sh
```
