# endgame302/skillshare fork

This fork publishes **patched builds of runkids/skillshare** under the same version numbers.

**Branches:**
- `main` mirrors upstream and gets no fork commits.
- `patched` (the default branch) is the latest upstream release plus the commits below.

**Patches carried:**

| Commit | Upstream status |
|---|---|
| `fix(sync): never delete or re-point entries skillshare did not create` | runkids/skillshare#914 |
| `docs: say what sync and init keep when skillshare did not create it` | runkids/skillshare#914 |
| `chore(fork): …` (this folder, the release workflow, `githubRepo` as a var) | fork only |
| `test(sourcefs): drop the raw-write allowance replaceIdenticalCopies no longer needs` | runkids/skillshare#914 |
| `feat(fork): add a Marketplaces slot to the Hubs page` | fork only |
| `feat(fork): search GitHub for skills on the Hubs page` | fork only |
| `feat(fork): browse plain skill repos as hubs` (also `search --hub owner/repo`) | fork only |
| `feat(fork): browse skills.sh on the Hubs page` | fork only |
| `feat(fork): browse and install ClawHub skills on the Hubs page` | fork only |
| `docs(fork): list the marketplace patches` | fork only |
| `fix(gitenv): never let git wait on a credential prompt nobody sees` | upstream candidate (no PR yet) |
| `refactor(fork): drop plainhub's own GCM_INTERACTIVE workaround` | fork only (needs the gitenv fix) |
| `perf(search): batch GitHub lookups through GraphQL and cache them` | upstream candidate (no PR yet) |
| `feat(fork): install ClawHub's GitHub-hosted skills, checked against the feed` | fork only |
| `feat(fork): translate the marketplace strings into every dashboard language` | fork only |
| `docs(fork): list the gitenv, search and ClawHub patches` | fork only |

**Marketplaces:** each marketplace is a `ui/src/components/hub/marketplaces/<id>.market.tsx` file, picked up automatically, with its strings in `ui/src/i18n/fork/<feature>.json`. Upstream's locale files are never edited, so the replay rarely conflicts there. Server routes live in `internal/server/handler_fork_*.go`, each registered with one line in `server.go`.

**How a release happens:** `.github/workflows/fork-release.yml` runs every Monday, or by hand from the Actions tab.
1. It replays `patched` onto the latest upstream release.
2. It runs upstream's `test.yaml` and `.fork/canary.ps1` on Windows.
3. It publishes a release here with GoReleaser. Builds set `githubRepo` to this fork, so `skillshare upgrade`, version checks and the dashboard download all come from these releases.

**When something fails:** if a patch stops applying, or a test or the canary fails, the workflow opens an issue and publishes nothing. When upstream merges a patch, the replay drops it automatically.

**To fix a conflict by hand:**

```sh
git fetch upstream --tags
git switch patched
git rebase vX.Y.Z        # resolve, then: git rebase --continue
git push --force-with-lease origin patched
```

Then run the workflow from the Actions tab with `force` on.
