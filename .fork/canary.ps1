# Canary test for the endgame302 fork: skillshare must not delete, change or re-point anything in a
# tool's skills folder that it did not create. Runs on Windows with junctions; no network needed.
#
# usage: pwsh -File .fork/canary.ps1 -Exe <path to skillshare.exe> [-Root <empty temp dir>]
# Exit code 0 = all canaries kept, 1 = something was removed or changed.
param([Parameter(Mandatory)][string]$Exe, [string]$Root = (Join-Path ([IO.Path]::GetTempPath()) "ss-canary-$PID"))
$ErrorActionPreference = 'Stop'
$Exe = (Resolve-Path $Exe).Path
if (Test-Path $Root) { throw "$Root exists" }
$failures = [System.Collections.Generic.List[string]]::new()

function Use-Home([string]$dir) {
  foreach ($d in 'home\.claude\skills', 'appdata\Roaming', 'appdata\Local', 'xdg', 'tmp') { New-Item -ItemType Directory -Force (Join-Path $dir $d) | Out-Null }
  $env:USERPROFILE = "$dir\home"; $env:HOME = "$dir\home"
  $env:APPDATA = "$dir\appdata\Roaming"; $env:LOCALAPPDATA = "$dir\appdata\Local"
  $env:XDG_CONFIG_HOME = "$dir\xdg"; $env:XDG_DATA_HOME = "$dir\xdg"; $env:XDG_STATE_HOME = "$dir\xdg"; $env:XDG_CACHE_HOME = "$dir\xdg"
  $env:TEMP = "$dir\tmp"; $env:TMP = "$dir\tmp"
  $env:SKILLSHARE_AUTO_UPDATE_CHECK = 'false'; $env:NO_COLOR = '1'
  Remove-Item Env:CLAUDE_CONFIG_DIR, Env:CODEX_HOME, Env:SKILLSHARE_CONFIG -ErrorAction SilentlyContinue
}
function New-Skill([string]$dir, [string]$name, [string]$text) {
  New-Item -ItemType Directory -Force $dir | Out-Null
  Set-Content (Join-Path $dir 'SKILL.md') "---`nname: $name`ndescription: $text`n---`n$text`n" -Encoding utf8
}
function Snap([string[]]$paths) {
  foreach ($p in $paths) {
    $i = Get-Item -Force -LiteralPath $p -ErrorAction SilentlyContinue
    if (-not $i) { "$p|missing"; continue }
    if ($i.LinkType) { "$p|$($i.LinkType)->$($i.Target -join '')"; continue }
    "$p|dir"
    Get-ChildItem -Force -Recurse -File -LiteralPath $p | Sort-Object FullName | ForEach-Object { "$($_.FullName)|$((Get-FileHash -LiteralPath $_.FullName).Hash)" }
  }
}
function Run([string]$label, [string[]]$cmdArgs) {
  $out = '' | & $Exe @cmdArgs 2>&1 | Out-String
  "  > skillshare $($cmdArgs -join ' ')  (exit $LASTEXITCODE)"
  if ($LASTEXITCODE -ne 0) { $failures.Add("$label failed with exit $LASTEXITCODE`n$out") }
}

# ---- Phase A: an existing source, a tool folder full of things skillshare did not make ----
$A = Join-Path $Root 'a'; Use-Home $A
$src = "$A\source"; $S = "$A\home\.claude\skills"; $ext = "$A\ext"
New-Skill "$src\alpha" 'alpha' 'source skill'
New-Skill "$src\xlsx" 'xlsx' 'source xlsx'
New-Skill "$src\docx" 'docx' 'source docx'
New-Skill "$S\team__shared" 'team__shared' 'hand-made, named like a flat skillshare folder'; Set-Content "$S\team__shared\MINE.txt" 'mine' -Encoding utf8
New-Item -ItemType Directory -Force "$S\_drafts" | Out-Null; Set-Content "$S\_drafts\notes.md" 'notes' -Encoding utf8
New-Skill "$S\docx" 'docx' 'my own docx, different from the source'
New-Skill "$ext\other-store\xlsx" 'xlsx' 'another manager owns this copy'
New-Skill "$ext\mine" 'mine' 'linked in by hand'
New-Item -ItemType Directory -Force "$ext\gone" | Out-Null
New-Item -ItemType Junction -Path "$S\xlsx" -Target "$ext\other-store\xlsx" | Out-Null
New-Item -ItemType Junction -Path "$S\my-link" -Target "$ext\mine" | Out-Null
New-Item -ItemType Junction -Path "$S\broken-link" -Target "$ext\gone" | Out-Null
Remove-Item -Recurse -Force "$ext\gone"
$canaries = 'team__shared', '_drafts', 'docx', 'xlsx', 'my-link', 'broken-link' | ForEach-Object { Join-Path $S $_ }
$before = @(Snap $canaries) + @(Snap "$ext\other-store", "$ext\mine")

"Phase A: init + sync twice + sync --dry-run"
Run 'init' @('init', '--source', $src, '--no-copy', '--no-git', '--no-skill', '--mode', 'merge', '--targets', 'claude')
Run 'sync 1' @('sync')
Run 'sync 2' @('sync')
Run 'sync dry-run' @('sync', '--dry-run')
$after = @(Snap $canaries) + @(Snap "$ext\other-store", "$ext\mine")
$diff = Compare-Object $before $after
if ($diff) { $failures.Add("Phase A canaries changed:`n" + ($diff | ForEach-Object { "  $($_.SideIndicator) $($_.InputObject)" } | Out-String)) } else { "  canaries unchanged: $($canaries.Count) entries" }
$alpha = Get-Item -Force "$S\alpha" -ErrorAction SilentlyContinue
if (-not ($alpha -and $alpha.LinkType)) { $failures.Add('Phase A: alpha was not linked from the source') } else { "  alpha linked ($($alpha.LinkType))" }

# ---- Phase B: init import replaces an identical copy with a link, original goes to the trash ----
$B = Join-Path $Root 'b'; Use-Home $B
$S2 = "$B\home\.claude\skills"
New-Skill "$S2\beta" 'beta' 'imported by init'
New-Item -ItemType Directory -Force "$B\ext\gone2" | Out-Null
New-Item -ItemType Junction -Path "$S2\broken-link" -Target "$B\ext\gone2" | Out-Null; Remove-Item -Recurse -Force "$B\ext\gone2"
$beforeB = @(Snap "$S2\broken-link")
"Phase B: init --copy-from claude"
Run 'init import' @('init', '--source', "$B\source", '--copy-from', 'claude', '--no-git', '--no-skill', '--mode', 'merge', '--targets', 'claude')
Run 'sync' @('sync')
if (Compare-Object $beforeB @(Snap "$S2\broken-link")) { $failures.Add('Phase B: the broken link skillshare did not make was changed') } else { '  broken link kept' }
$beta = Get-Item -Force "$S2\beta" -ErrorAction SilentlyContinue
if (-not ($beta -and $beta.LinkType)) { $failures.Add('Phase B: beta was not replaced by a link') } else { '  beta replaced by a link' }
$trashed = @(Get-ChildItem -Recurse -Force -Filter SKILL.md "$B\xdg\skillshare\trash" -ErrorAction SilentlyContinue | Where-Object { (Get-Content $_.FullName -Raw) -match 'imported by init' })
if ($trashed.Count -lt 1) { $failures.Add('Phase B: the original beta folder is not in the trash') } else { "  original beta in trash: $($trashed[0].Directory.FullName.Substring($B.Length))" }

if ($failures.Count) { "`nCANARY FAILED:"; $failures | ForEach-Object { $_ }; exit 1 }
"`nCANARY PASSED"
exit 0
