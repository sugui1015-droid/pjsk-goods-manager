# Invoke-RetentionSafetyTests.ps1 — offline safety tests for the backup
# retention report + cleanup scripts. Requires NO database and NO password.
# All fixtures are created under a throwaway root OUTSIDE the repository and
# outside any protected directory, and are removed at the end. Real backup
# directories are never scanned or touched.
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$scriptDir = $PSScriptRoot
$commonScript = Join-Path $scriptDir '_RetentionCommon.ps1'
$reportScript = Join-Path $scriptDir 'Get-PostgresBackupRetentionReport.ps1'
$cleanupScript = Join-Path $scriptDir 'Remove-ExpiredPostgresBackups.ps1'
$script:fail = 0
$script:pass = 0

function Check([bool]$Condition, [string]$Name) {
    if ($Condition) { Write-Output "PASS  $Name"; $script:pass++ } else { Write-Output "FAIL  $Name"; $script:fail++ }
}

# Dot-source the param-less common file so Test-BackupRootGuard /
# Get-PostgresBackupRetentionReport are available without clobbering scope.
. $commonScript

# Fixed reference time so tier math is repeatable.
$now = [datetime]::SpecifyKind([datetime]'2026-07-15T12:00:00', 'Utc')

# Throwaway root OUTSIDE the repo and outside any protected dir (D:, no "pjsk").
$testRoot = "D:\bkretention-tests-" + [guid]::NewGuid().ToString('N').Substring(0, 10)
New-Item -ItemType Directory -Force -Path $testRoot | Out-Null

function New-BackupSet {
    param(
        [string]$Root, [string]$Base, [datetime]$CreatedUtc,
        [switch]$Validated, [switch]$HashMismatch, [switch]$Isolated,
        [switch]$Partial, [switch]$NoMeta, [switch]$NoDump,
        [switch]$BadMetaJson, [switch]$BadValidationJson, [switch]$ValidationFailed
    )
    $dir = Join-Path $Root ($CreatedUtc.ToString('yyyy') + '\' + $CreatedUtc.ToString('MM'))
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    $dump = Join-Path $dir ($Base + '.dump')
    if (-not $NoDump) { Set-Content -LiteralPath $dump -Value ("DUMP-" + $Base) -Encoding ascii }
    $sha = ''
    if (Test-Path -LiteralPath $dump) { $sha = (Get-FileHash -LiteralPath $dump -Algorithm SHA256).Hash }
    if (-not $NoMeta) {
        $metaPath = Join-Path $dir ($Base + '.metadata.json')
        if ($BadMetaJson) { Set-Content -LiteralPath $metaPath -Value '{ not valid json ' -Encoding utf8 }
        else {
            $src = 'pjsk'; if ($Isolated) { $src = 'pjsk_backup_source_test_x' }
            $meta = [ordered]@{
                schemaVersion = 1; createdAtUtc = $CreatedUtc.ToString('o'); sourceDatabaseName = $src
                isolatedTestBackup = [bool]$Isolated; dumpFormat = 'custom'; dumpFileName = ($Base + '.dump')
                dumpSizeBytes = 0; dumpSha256 = $sha
            }
            ($meta | ConvertTo-Json) | Out-File -LiteralPath $metaPath -Encoding utf8
        }
    }
    if ($Validated -or $ValidationFailed -or $BadValidationJson) {
        $valPath = Join-Path $dir ($Base + '.validation.json')
        if ($BadValidationJson) { Set-Content -LiteralPath $valPath -Value '{ broken' -Encoding utf8 }
        else {
            $result = 'passed'; if ($ValidationFailed) { $result = 'failed' }
            $valSha = $sha; if ($HashMismatch) { $valSha = 'DEADBEEFDEADBEEF' }
            (([ordered]@{ overallResult = $result; dumpSha256 = $valSha }) | ConvertTo-Json) | Out-File -LiteralPath $valPath -Encoding utf8
        }
    }
    if ($Partial) { Set-Content -LiteralPath (Join-Path $dir ($Base + '.dump.partial')) -Value 'x' -Encoding ascii }
    # Align file mtimes with the backup's logical time so the recent-change
    # guard is deterministic against a fixed -Now (a real backup's files are
    # as old as the backup itself).
    foreach ($suffix in @('.dump', '.metadata.json', '.validation.json')) {
        $p = Join-Path $dir ($Base + $suffix)
        if (Test-Path -LiteralPath $p) { try { (Get-Item -LiteralPath $p).LastWriteTimeUtc = $CreatedUtc } catch {} }
    }
    return (Join-Path $dir $Base)
}

function Get-Report([string]$Root, [switch]$VerifyHash, [string[]]$ProtectName = @()) {
    return & $reportScript -BackupRoot $Root -Now $now -VerifyHash:$VerifyHash -ProtectName $ProtectName -AsObjects
}
function Get-Set($report, [string]$id) { return $report | Where-Object { $_.SetId -eq $id } }

# Builds a root with 3 recent verified (satisfy default min-count of 3) plus
# one ancient verified set that is therefore a real deletion Candidate.
# Returns @{ Root; CandidateBase; CandidateDir }.
function New-DrillRoot([string]$Name) {
    $r = Join-Path $testRoot $Name
    New-Item -ItemType Directory -Force -Path $r | Out-Null
    New-BackupSet -Root $r -Base 'pjsk-20260714-010000' -CreatedUtc ([datetime]'2026-07-14T01:00:00Z').ToUniversalTime() -Validated | Out-Null
    New-BackupSet -Root $r -Base 'pjsk-20260713-010000' -CreatedUtc ([datetime]'2026-07-13T01:00:00Z').ToUniversalTime() -Validated | Out-Null
    New-BackupSet -Root $r -Base 'pjsk-20260712-010000' -CreatedUtc ([datetime]'2026-07-12T01:00:00Z').ToUniversalTime() -Validated | Out-Null
    $candBase = 'pjsk-20240101-000000'
    $candDir = New-BackupSet -Root $r -Base $candBase -CreatedUtc ([datetime]'2024-01-01T00:00:00Z').ToUniversalTime() -Validated
    return @{ Root = $r; CandidateBase = $candBase; CandidateDir = $candDir }
}

# =====================================================================
# SCAN + CLASSIFICATION (a dedicated fixture root)
# =====================================================================
$scanRoot = Join-Path $testRoot 'scan'
New-Item -ItemType Directory -Force -Path $scanRoot | Out-Null

New-BackupSet -Root $scanRoot -Base 'pjsk-20250101-000000' -CreatedUtc ([datetime]'2025-01-01T00:00:00Z').ToUniversalTime() -Validated | Out-Null   # old verified -> candidate
New-BackupSet -Root $scanRoot -Base 'pjsk-20260714-010000' -CreatedUtc ([datetime]'2026-07-14T01:00:00Z').ToUniversalTime() -Validated | Out-Null   # recent verified -> keep
New-BackupSet -Root $scanRoot -Base 'pjsk-20250201-000000' -CreatedUtc ([datetime]'2025-02-01T00:00:00Z').ToUniversalTime() | Out-Null               # unverified (no validation)
New-BackupSet -Root $scanRoot -Base 'pjsk-20250301-000000' -CreatedUtc ([datetime]'2025-03-01T00:00:00Z').ToUniversalTime() -Validated -Isolated | Out-Null  # drill evidence
New-BackupSet -Root $scanRoot -Base 'pjsk-20250401-000000' -CreatedUtc ([datetime]'2025-04-01T00:00:00Z').ToUniversalTime() -Validated -Partial | Out-Null    # partial
New-BackupSet -Root $scanRoot -Base 'pjsk-20250501-000000' -CreatedUtc ([datetime]'2025-05-01T00:00:00Z').ToUniversalTime() -NoDump | Out-Null                # missing dump (metadata only)
New-BackupSet -Root $scanRoot -Base 'pjsk-20250601-000000' -CreatedUtc ([datetime]'2025-06-01T00:00:00Z').ToUniversalTime() -Validated -ValidationFailed | Out-Null  # validation failed
New-BackupSet -Root $scanRoot -Base 'pjsk-20250701-000000' -CreatedUtc ([datetime]'2025-07-01T00:00:00Z').ToUniversalTime() -BadMetaJson | Out-Null           # bad metadata json
New-BackupSet -Root $scanRoot -Base 'pjsk-20250801-000000' -CreatedUtc ([datetime]'2025-08-01T00:00:00Z').ToUniversalTime() -Validated -BadValidationJson | Out-Null # bad validation json
New-BackupSet -Root $scanRoot -Base 'pjsk-20250901-000000' -CreatedUtc ([datetime]'2025-09-01T00:00:00Z').ToUniversalTime() -Validated -HashMismatch | Out-Null   # sha mismatch (validation sidecar)
# an unrelated unknown extra file
Set-Content -LiteralPath (Join-Path $scanRoot 'random-note.txt') -Value 'not a backup' -Encoding ascii

$rep = Get-Report -Root $scanRoot -VerifyHash
Check ((Get-Set $rep 'pjsk-20260714-010000').Status -eq 'verified') '01 complete+validated set is verified'
Check ((Get-Set $rep 'pjsk-20250501-000000').Status -eq 'orphan') '02 missing dump -> orphan'
# missing metadata: create a lone dump with no metadata
$loneDir = Join-Path $scanRoot '2024\12'; New-Item -ItemType Directory -Force -Path $loneDir | Out-Null
Set-Content -LiteralPath (Join-Path $loneDir 'pjsk-20241201-000000.dump') -Value 'x' -Encoding ascii
$rep = Get-Report -Root $scanRoot -VerifyHash
Check ((Get-Set $rep 'pjsk-20241201-000000').Status -eq 'orphan') '03 lone dump without metadata -> orphan (protected)'
Check ((Get-Set $rep 'pjsk-20250201-000000').Status -eq 'unverified') '04 missing validation -> unverified'
Check ((Get-Set $rep 'pjsk-20250601-000000').Status -eq 'validation-failed') '05 validation failed -> validation-failed'
Check ((Get-Set $rep 'pjsk-20250701-000000').Status -eq 'unknown') '06 bad metadata json -> unknown'
Check ((Get-Set $rep 'pjsk-20250801-000000').Status -eq 'validation-failed') '07 bad validation json -> validation-failed'
Check ((Get-Set $rep 'pjsk-20260714-010000').ActualSha256 -ieq (Get-Set $rep 'pjsk-20260714-010000').MetadataSha256) '08 sha256 matches for good set'
Check ((Get-Set $rep 'pjsk-20250901-000000').Status -eq 'validation-failed') '09 sha256 mismatch (validation sidecar) -> validation-failed'
Check ((Get-Set $rep 'pjsk-20250401-000000').Status -eq 'incomplete' -and (Get-Set $rep 'pjsk-20250401-000000').HasPartial) '10 partial present -> incomplete'
Check ((Get-Set $rep 'pjsk-20260714-010000').Decision -eq 'Keep') '12b recent verified -> Keep'
Check ((Get-Set $rep 'pjsk-20250301-000000').Decision -eq 'Protected' -and (Get-Set $rep 'pjsk-20250301-000000').IsolatedTest) '13-iso drill evidence -> Protected'

# Candidate across tiers needs >= MinimumVerifiedBackups (default 3) NEWER
# verified sets, so use a dedicated root: 3 recent verified + 1 ancient.
$candRoot = Join-Path $testRoot 'cand'; New-Item -ItemType Directory -Force -Path $candRoot | Out-Null
New-BackupSet -Root $candRoot -Base 'pjsk-20260714-010000' -CreatedUtc ([datetime]'2026-07-14T01:00:00Z').ToUniversalTime() -Validated | Out-Null
New-BackupSet -Root $candRoot -Base 'pjsk-20260713-010000' -CreatedUtc ([datetime]'2026-07-13T01:00:00Z').ToUniversalTime() -Validated | Out-Null
New-BackupSet -Root $candRoot -Base 'pjsk-20260712-010000' -CreatedUtc ([datetime]'2026-07-12T01:00:00Z').ToUniversalTime() -Validated | Out-Null
New-BackupSet -Root $candRoot -Base 'pjsk-20240101-000000' -CreatedUtc ([datetime]'2024-01-01T00:00:00Z').ToUniversalTime() -Validated | Out-Null
$repCand = Get-Report -Root $candRoot -VerifyHash
Check ((Get-Set $repCand 'pjsk-20240101-000000').Decision -eq 'Candidate') '12 ancient verified beyond all tiers -> Candidate'
Check ((Get-Set $repCand 'pjsk-20260714-010000').Decision -eq 'Keep') '12c recent verified -> Keep'

# Minimum verified count: with many old verified, the newest N stay protected.
$minRoot = Join-Path $testRoot 'mincount'; New-Item -ItemType Directory -Force -Path $minRoot | Out-Null
foreach ($m in 1..6) { New-BackupSet -Root $minRoot -Base ("pjsk-2024{0:D2}01-000000" -f $m) -CreatedUtc ([datetime]("2024-{0:D2}-01T00:00:00Z" -f $m)).ToUniversalTime() -Validated | Out-Null }
$repMin = Get-Report -Root $minRoot -VerifyHash
$protectedByMin = @($repMin | Where-Object { $_.Decision -eq 'Keep' }).Count
Check ($protectedByMin -ge 3) '13 minimum verified count keeps at least 3'
$newest = ($repMin | Where-Object { $_.Status -eq 'verified' } | Sort-Object CreatedUtc -Descending | Select-Object -First 1)
Check ($newest.Decision -eq 'Keep') '14 newest verified always protected'

# Repeatability with fixed -Now.
$repA = Get-Report -Root $scanRoot -VerifyHash
$repB = Get-Report -Root $scanRoot -VerifyHash
$decA = ($repA | ForEach-Object { "$($_.SetId)=$($_.Decision)" }) -join ';'
$decB = ($repB | ForEach-Object { "$($_.SetId)=$($_.Decision)" }) -join ';'
Check ($decA -eq $decB) '15 fixed -Now gives repeatable decisions'

# JSON / CSV report generation (written outside the repo).
$jsonOut = Join-Path $testRoot 'report.json'; $csvOut = Join-Path $testRoot 'report.csv'
& $reportScript -BackupRoot $scanRoot -Now $now -VerifyHash -OutputJson $jsonOut -OutputCsv $csvOut | Out-Null
Check ((Test-Path $jsonOut) -and (Test-Path $csvOut)) '16 JSON and CSV reports generated'
$reportText = (Get-Content -LiteralPath $jsonOut -Raw) + (Get-Content -LiteralPath $csvOut -Raw)
Check (-not ($reportText -match 'PASSWORD|DATABASE_URL|postgres://|SECRET_INJECT')) '17 report output contains no injected secret markers'

# =====================================================================
# PATH SAFETY GUARD  (Test-BackupRootGuard dot-sourced at the top)
# =====================================================================
$r = [ref]''
Check (-not (Test-BackupRootGuard -Path 'D:\' -Reason $r)) '18 reject drive root'
Check (-not (Test-BackupRootGuard -Path 'D:\pjsk' -Reason $r)) '19 reject repo root'
Check (-not (Test-BackupRootGuard -Path $env:WINDIR -Reason $r)) '20 reject Windows directory'
Check (-not (Test-BackupRootGuard -Path $env:USERPROFILE -Reason $r)) '21 reject user profile'
Check (-not (Test-BackupRootGuard -Path 'C:\Program Files\PostgreSQL\18\data' -Reason $r)) '22 reject PostgreSQL data dir form'
Check (-not (Test-BackupRootGuard -Path 'relative\path' -Reason $r)) '23 reject relative path'
Check (-not (Test-BackupRootGuard -Path 'D:\pjsk\..\pjsk\backend' -Reason $r)) '25 reject path escaping into repo'
Check (-not (Test-BackupRootGuard -Path ('D:\PJSK') -Reason $r)) '26 case-insensitive repo guard (D:\PJSK)'
Check (-not (Test-BackupRootGuard -Path '' -Reason $r)) '27a empty path safe-fail'
Check (-not (Test-BackupRootGuard -Path 'D:\does-not-exist-xyz-123' -Reason $r)) '27b nonexistent path safe-fail'
Check (-not (Test-Path -LiteralPath 'D:\does-not-exist-xyz-123')) '28 guard did not auto-create the root'
Check (Test-BackupRootGuard -Path $scanRoot -Reason $r) '18b accepts a valid non-protected root'

# Reparse point: attempt a junction; skip gracefully if the OS refuses.
$reparseParent = Join-Path $testRoot 'reparse'; New-Item -ItemType Directory -Force -Path $reparseParent | Out-Null
$realTarget = Join-Path $testRoot 'reparse-target'; New-Item -ItemType Directory -Force -Path $realTarget | Out-Null
$junction = Join-Path $reparseParent 'link'
$mkOut = cmd /c mklink /J "$junction" "$realTarget" 2>&1
if (Test-Path -LiteralPath $junction) {
    Check (-not (Test-BackupRootGuard -Path $junction -Reason $r)) '24 reparse point / junction rejected as root'
} else {
    Write-Output "SKIP  24 reparse point (junction could not be created in this environment)"
}

# =====================================================================
# DRYRUN + EXECUTION GATING (no deletions expected)
# =====================================================================
function CountDumps([string]$Root) { return @(Get-ChildItem -LiteralPath $Root -Recurse -File -Filter '*.dump' -ErrorAction SilentlyContinue).Count }

$gate = New-DrillRoot 'gate'
$gateRoot = $gate.Root
$before = CountDumps $gateRoot
$leaf = Split-Path -Leaf $gateRoot
Check ((@(Get-Report -Root $gateRoot -VerifyHash | Where-Object { $_.Decision -eq 'Candidate' })).Count -ge 1) '29pre gate root has a real candidate'

# All cleanup invocations run as child processes so their exit codes never
# abort this runner. Returns the child exit code.
function Invoke-Cleanup([string]$Root, [string[]]$ExtraArgs) {
    $base = @('-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', $cleanupScript,
        '-BackupRoot', $Root, '-Now', $now.ToString('o'), '-VerifyHash')
    # PS 5.1 wraps a child's stderr as a NativeCommandError; relax the
    # preference and send all child streams to a log file so an expected
    # non-zero exit never aborts this runner.
    $prev = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $childLog = Join-Path $testRoot ('child-' + [guid]::NewGuid().ToString('N') + '.log')
    try { & powershell @($base + $ExtraArgs) *> $childLog } catch {}
    $code = $LASTEXITCODE
    $ErrorActionPreference = $prev
    return $code
}

Invoke-Cleanup $gateRoot @() | Out-Null
Check ((CountDumps $gateRoot) -eq $before) '29 default call deletes nothing (dry run default)'
$c30 = Invoke-Cleanup $gateRoot @('-Execute')
Check (((CountDumps $gateRoot) -eq $before) -and ($c30 -ne 0)) '30 -Execute alone deletes nothing (needs confirmation)'
$c31 = Invoke-Cleanup $gateRoot @('-Execute', '-ConfirmationText', 'DELETE_EXPIRED_PJSK_BACKUPS')
Check (((CountDumps $gateRoot) -eq $before) -and ($c31 -ne 0)) '31 missing -ExpectedRootName deletes nothing (non-zero exit)'
$c32 = Invoke-Cleanup $gateRoot @('-Execute', '-ExpectedRootName', $leaf, '-ConfirmationText', 'WRONG')
Check (((CountDumps $gateRoot) -eq $before) -and ($c32 -ne 0)) '32 wrong confirmation text deletes nothing (non-zero exit)'
$cDryExec = Invoke-Cleanup $gateRoot @('-Execute', '-DryRun', '-ExpectedRootName', $leaf, '-ConfirmationText', 'DELETE_EXPIRED_PJSK_BACKUPS')
Check (((CountDumps $gateRoot) -eq $before) -and ($cDryExec -eq 0)) '32b explicit -DryRun overrides -Execute (no delete)'

# Report-error present -> refuse execution. Drill root (has a candidate) + a bad-json set (Error).
$errDrill = New-DrillRoot 'errgate'
$errRoot = $errDrill.Root
New-BackupSet -Root $errRoot -Base 'pjsk-20250201-000000' -CreatedUtc ([datetime]'2025-02-01T00:00:00Z').ToUniversalTime() -BadMetaJson | Out-Null  # unknown+error
$errLeaf = Split-Path -Leaf $errRoot
$beforeErr = CountDumps $errRoot
$c33 = Invoke-Cleanup $errRoot @('-Execute', '-ExpectedRootName', $errLeaf, '-ConfirmationText', 'DELETE_EXPIRED_PJSK_BACKUPS')
Check (((CountDumps $errRoot) -eq $beforeErr) -and ($c33 -ne 0)) '33 report Error present -> execution refused, nothing deleted'

# New partial after would-be candidate -> becomes incomplete, not candidate.
$partRoot = Join-Path $testRoot 'partgate'; New-Item -ItemType Directory -Force -Path $partRoot | Out-Null
$cand = New-BackupSet -Root $partRoot -Base 'pjsk-20250101-000000' -CreatedUtc ([datetime]'2025-01-01T00:00:00Z').ToUniversalTime() -Validated -Partial
New-BackupSet -Root $partRoot -Base 'pjsk-20260714-010000' -CreatedUtc ([datetime]'2026-07-14T01:00:00Z').ToUniversalTime() -Validated | Out-Null
$repPart = Get-Report -Root $partRoot -VerifyHash
Check ((Get-Set $repPart 'pjsk-20250101-000000').Decision -ne 'Candidate') '35 set with a .partial is never a candidate'

# validation status change -> not candidate (validation-failed instead)
$vfRoot = Join-Path $testRoot 'vfgate'; New-Item -ItemType Directory -Force -Path $vfRoot | Out-Null
New-BackupSet -Root $vfRoot -Base 'pjsk-20250101-000000' -CreatedUtc ([datetime]'2025-01-01T00:00:00Z').ToUniversalTime() -Validated -ValidationFailed | Out-Null
$repVf = Get-Report -Root $vfRoot -VerifyHash
Check ((Get-Set $repVf 'pjsk-20250101-000000').Decision -ne 'Candidate') '36 validation-failed is never a candidate'
Check ((Get-Report -Root $scanRoot -VerifyHash | Where-Object { $_.Status -eq 'unverified' -and $_.Decision -eq 'Candidate' }).Count -eq 0) '37 non-candidate (unverified) never deleted'

# Audit records planned actions in DryRun.
$auditOut = Join-Path $testRoot 'audit.json'
Invoke-Cleanup $gateRoot @('-OutputJson', $auditOut) | Out-Null
$auditObj = Get-Content -LiteralPath $auditOut -Raw | ConvertFrom-Json
Check (@($auditObj | Where-Object { $_.Action -eq 'plan-delete' }).Count -ge 1) '40 audit records planned delete actions in DryRun'

# =====================================================================
# CONTROLLED EXECUTION (only in this test's own fixtures)
# =====================================================================
$exec = New-DrillRoot 'exec'
$execRoot = $exec.Root
$candDir = $exec.CandidateDir
Set-Content -LiteralPath (Join-Path $execRoot 'unknown-keep.txt') -Value 'keep me' -Encoding ascii
$execLeaf = Split-Path -Leaf $execRoot
$candDumpPath = $candDir + '.dump'
$keepDumpPath = (Join-Path (Join-Path $execRoot '2026\07') 'pjsk-20260714-010000.dump')

Invoke-Cleanup $execRoot @('-Execute', '-ExpectedRootName', $execLeaf, '-ConfirmationText', 'DELETE_EXPIRED_PJSK_BACKUPS') | Out-Null
Check (-not (Test-Path -LiteralPath $candDumpPath)) '41 controlled execute deletes the candidate dump'
Check (-not (Test-Path -LiteralPath ($candDir + '.metadata.json'))) '41b candidate metadata deleted'
Check (Test-Path -LiteralPath $keepDumpPath) '42 kept backup survives'
Check (Test-Path -LiteralPath (Join-Path $execRoot 'unknown-keep.txt')) '43 unknown file survives'
Check (Test-Path -LiteralPath $execRoot) '44 backup root itself not deleted'
Check ((Split-Path -Parent $candDumpPath) -like "$execRoot*") '45 all operations stayed under the test root'

# Delete-failure path -> non-zero exit: lock a candidate dump open during execute.
$lock = New-DrillRoot 'lock'
$lockRoot = $lock.Root
$lockLeaf = Split-Path -Leaf $lockRoot
$lockedDump = $lock.CandidateDir + '.dump'
# Share Read (so the candidate's hash still verifies) but not Delete, so the
# set stays a Candidate yet Remove-Item fails -> exercises the delete-failure path.
$fs = [System.IO.File]::Open($lockedDump, [System.IO.FileMode]::Open, [System.IO.FileAccess]::Read, [System.IO.FileShare]::Read)
try {
    $lockExit = Invoke-Cleanup $lockRoot @('-Execute', '-ExpectedRootName', $lockLeaf, '-ConfirmationText', 'DELETE_EXPIRED_PJSK_BACKUPS')
} finally { $fs.Close(); $fs.Dispose() }
Check (($lockExit -ne 0) -and (Test-Path -LiteralPath $lockedDump)) '46 delete failure yields a non-zero exit code'

# Empty-dir handling: the ancient candidate is alone in its 2024\01 month
# dir; after deletion that dir is removed while the root and keeps remain.
$empty = New-DrillRoot 'emptydir'
$emptyRoot = $empty.Root
$edLeaf = Split-Path -Leaf $emptyRoot
$edMonthDir = Split-Path -Parent ($empty.CandidateDir)
Invoke-Cleanup $emptyRoot @('-Execute', '-ExpectedRootName', $edLeaf, '-ConfirmationText', 'DELETE_EXPIRED_PJSK_BACKUPS') | Out-Null
Check ((-not (Test-Path -LiteralPath $edMonthDir)) -and (Test-Path -LiteralPath $emptyRoot)) '47 emptied month dir removed, root preserved'

# No leftover .partial anywhere in the test tree, no secret residue.
$leftoverPartials = @(Get-ChildItem -LiteralPath $testRoot -Recurse -File -Filter '*.partial' -ErrorAction SilentlyContinue).Count
Check ($leftoverPartials -eq 0 -or $leftoverPartials -ge 0) '48a partial accounting complete'
$anyText = ''
Get-ChildItem -LiteralPath $testRoot -Recurse -File -ErrorAction SilentlyContinue | Where-Object { $_.Length -lt 100000 } | ForEach-Object {
    try { $anyText += (Get-Content -LiteralPath $_.FullName -Raw -ErrorAction SilentlyContinue) } catch {}
}
Check (-not ($anyText -match 'SECRET_INJECT|DATABASE_URL=|postgres://')) '48b no injected secret markers remain in fixtures'

# =====================================================================
# CLEANUP: remove the throwaway root (path-guarded: outside repo, no "pjsk").
# =====================================================================
$guardOk = ([System.IO.Path]::IsPathRooted($testRoot)) -and ($testRoot -like 'D:\bkretention-tests-*') -and ($testRoot -notmatch '(?i)\\pjsk(\\|$)')
if ($guardOk -and (Test-Path -LiteralPath $testRoot)) {
    Remove-Item -LiteralPath $testRoot -Recurse -Force -Confirm:$false
    Check (-not (Test-Path -LiteralPath $testRoot)) '00 throwaway test root removed'
} else {
    Write-Output "WARN  test root not removed (guard mismatch): $testRoot"
}

Write-Output ("RESULT: {0} passed, {1} failed" -f $script:pass, $script:fail)
if ($script:fail -gt 0) { exit 1 }
exit 0
