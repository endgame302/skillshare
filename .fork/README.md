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
