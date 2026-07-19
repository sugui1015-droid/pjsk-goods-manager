# Export one PostgreSQL database, its aggregate baseline, and its exact migration
# set from one PostgreSQL exported snapshot. No password is accepted as text.
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$SourceHost,
    [Parameter(Mandatory)][ValidateRange(1,65535)][int]$SourcePort,
    [Parameter(Mandatory)][string]$SourceDatabase,
    [Parameter(Mandatory)][string]$SourceUser,
    [Parameter(Mandatory)][string]$OutputRoot,
    [Parameter(Mandatory)][string]$PgBinDirectory,
    [Parameter(Mandatory)][ValidateSet('rehearsal','cutover')][string]$Mode,
    [string]$Passfile,
    [switch]$WriteFreezeConfirmed,
    [string]$MaintenanceWindowId
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0
. (Join-Path $PSScriptRoot '_SnapshotCoordinator.ps1')

$utf8NoBom = New-Object System.Text.UTF8Encoding($false)
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$stagingDirectory = $null
$finalDirectory = $null
$coordinator = $null
$transactionOpen = $false
$directoryRenameCompleted = $false
$published = $false
$failureStage = 'preflight'
$runId = $null

function Write-Utf8NoBom([string]$Path, [string]$Text) {
    [System.IO.File]::WriteAllText($Path, $Text, $utf8NoBom)
}

function Invoke-CheckedNative {
    param([string]$Path, [string[]]$Arguments, [string]$Stage, [switch]$ReturnLines)
    $oldPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $output = @(& $Path @Arguments 2>&1 | ForEach-Object { "$_" })
        $code = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $oldPreference
    }
    if ($code -ne 0) { throw "$Stage failed with exit code $code." }
    if ($ReturnLines) { return @($output) }
    return ($output -join ' ')
}

function Invoke-TestFailureInjection([string]$Stage) {
    if ($env:PJSK_SNAPSHOT_TEST_FAIL_STAGE -ceq $Stage) {
        throw "Injected snapshot test failure at $Stage."
    }
}

function New-PsqlCoordinator {
    param([string]$PsqlPath, [string]$PassfilePath)
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $PsqlPath
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    $psi.RedirectStandardInput = $true
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.Arguments = ('-X -w --quiet --no-align --tuples-only --set ON_ERROR_STOP=on --host "{0}" --port {1} --username "{2}" --dbname "{3}"' -f $SourceHost,$SourcePort,$SourceUser,$SourceDatabase)
    if ($PassfilePath) { $psi.EnvironmentVariables['PGPASSFILE'] = $PassfilePath }
    $process = New-Object System.Diagnostics.Process
    $process.StartInfo = $psi
    if (-not $process.Start()) { throw 'Could not start the PostgreSQL coordinator connection.' }
    return $process
}

function Invoke-CoordinatorQuery {
    param([System.Diagnostics.Process]$Process, [string]$Label, [string]$Sql)
    if ($Process.HasExited) { throw "Coordinator exited before $Label." }
    $token = [Guid]::NewGuid().ToString('N')
    $begin = "__PJSK_BEGIN_${token}__"
    $end = "__PJSK_END_${token}__"
    $Process.StandardInput.WriteLine("\echo $begin")
    $Process.StandardInput.WriteLine("/* PJSK:$Label */ $Sql")
    $Process.StandardInput.WriteLine("\echo $end")
    $Process.StandardInput.Flush()
    $found = $false
    $lines = New-Object System.Collections.Generic.List[string]
    while ($true) {
        $line = $Process.StandardOutput.ReadLine()
        if ($null -eq $line) {
            $stderr = $Process.StandardError.ReadToEnd()
            throw "Coordinator ended during $Label. $stderr"
        }
        if ($line -eq $begin) { $found = $true; continue }
        if ($line -eq $end -and $found) { break }
        if ($found -and $line -ne '') { $lines.Add($line) }
    }
    return $lines.ToArray()
}

function Parse-KeyRows([string[]]$Lines, [int]$ExpectedParts) {
    $result = New-Object System.Collections.Generic.List[object]
    foreach ($line in $Lines) {
        $parts = @($line -split '\|', $ExpectedParts)
        if ($parts.Count -ne $ExpectedParts) { throw 'Aggregate query returned an unexpected shape.' }
        $result.Add([pscustomobject]@{ Values=$parts })
    }
    return $result.ToArray()
}

function Get-AggregateBaseline {
    param([System.Diagnostics.Process]$Process, [string]$RunId, [string]$ExportedAtUtc, [string]$ServerVersion, [string[]]$Migrations)
    $tableNames = @(Invoke-CoordinatorQuery $Process 'TABLE_CATALOG' "select c.relname from pg_class c join pg_namespace n on n.oid=c.relnamespace where n.nspname='public' and c.relkind in ('r','p') order by c.relname collate `"C`";")
    if ($tableNames.Count -eq 0) { throw 'No public source tables were found.' }
    $counts = [ordered]@{}
    foreach ($table in $tableNames) {
        $q = ConvertTo-PgIdentifier $table
        $value = @(Invoke-CoordinatorQuery $Process 'TABLE_COUNT' "select count(*)::text from public.$q;")
        if ($value.Count -ne 1 -or $value[0] -notmatch '^[0-9]+$') { throw 'Table count query failed validation.' }
        $counts[$table] = [ordered]@{ total = "$($value[0])" }
    }

    $status = [ordered]@{}
    $statusColumns = Parse-KeyRows @(Invoke-CoordinatorQuery $Process 'STATUS_CATALOG' "select table_name||'|'||column_name from information_schema.columns where table_schema='public' and column_name in ('status','payment_status') and data_type in ('text','character varying') order by table_name collate `"C`",column_name collate `"C`";") 2
    foreach ($parts in $statusColumns) {
        $table = $parts.Values[0]; $column = $parts.Values[1]
        $qt = ConvertTo-PgIdentifier $table; $qc = ConvertTo-PgIdentifier $column
        $distribution = [ordered]@{}
        $rows = Parse-KeyRows @(Invoke-CoordinatorQuery $Process 'STATUS_DISTRIBUTION' "select coalesce($qc,'__NULL__')||'|'||count(*)::text from public.$qt group by $qc order by coalesce($qc,'__NULL__') collate `"C`";") 2
        foreach ($row in $rows) {
            if ($row.Values[0] -notmatch '^(?:__[A-Z]+__|[a-z0-9_-]+)$' -or $row.Values[1] -notmatch '^[0-9]+$') { throw 'Status distribution contained an unsafe value.' }
            $distribution[$row.Values[0]] = "$($row.Values[1])"
        }
        $status["$table.$column"] = $distribution
    }

    $binary = [ordered]@{}
    $binaryColumns = Parse-KeyRows @(Invoke-CoordinatorQuery $Process 'BYTEA_CATALOG' "select table_name||'|'||column_name from information_schema.columns where table_schema='public' and data_type='bytea' order by table_name collate `"C`",column_name collate `"C`";") 2
    foreach ($parts in $binaryColumns) {
        $table=$parts.Values[0]; $column=$parts.Values[1]; $qt=ConvertTo-PgIdentifier $table; $qc=ConvertTo-PgIdentifier $column
        $values = Parse-KeyRows @(Invoke-CoordinatorQuery $Process 'BYTEA_AGGREGATE' "select count(*)::text||'|'||count($qc)::text||'|'||count(*) filter (where $qc is not null and octet_length($qc)>0)::text from public.$qt;") 3
        $binary["$table.$column"] = [ordered]@{ total="$($values[0].Values[0])"; nonNull="$($values[0].Values[1])"; nonEmpty="$($values[0].Values[2])" }
    }

    $numeric = [ordered]@{}
    $numericColumns = Parse-KeyRows @(Invoke-CoordinatorQuery $Process 'NUMERIC_CATALOG' "select table_name||'|'||column_name from information_schema.columns where table_schema='public' and data_type='numeric' order by table_name collate `"C`",column_name collate `"C`";") 2
    foreach ($parts in $numericColumns) {
        $table=$parts.Values[0]; $column=$parts.Values[1]; $qt=ConvertTo-PgIdentifier $table; $qc=ConvertTo-PgIdentifier $column
        $values = Parse-KeyRows @(Invoke-CoordinatorQuery $Process 'NUMERIC_AGGREGATE' "select count(*)::text||'|'||count($qc)::text||'|'||coalesce(sum($qc)::text,'__NULL__') from public.$qt;") 3
        if ($values[0].Values[2] -ne '__NULL__' -and $values[0].Values[2] -notmatch '^-?[0-9]+(?:\.[0-9]+)?$') { throw 'Numeric aggregate was not lossless text.' }
        $numeric["$table.$column"] = [ordered]@{ total="$($values[0].Values[0])"; nonNull="$($values[0].Values[1])"; sum=$(if($values[0].Values[2] -eq '__NULL__'){$null}else{"$($values[0].Values[2])"}) }
    }

    $timeBounds = [ordered]@{}
    $timeColumns = Parse-KeyRows @(Invoke-CoordinatorQuery $Process 'TIME_CATALOG' "select table_name||'|'||column_name from information_schema.columns where table_schema='public' and data_type='timestamp with time zone' order by table_name collate `"C`",column_name collate `"C`";") 2
    foreach ($parts in $timeColumns) {
        $table=$parts.Values[0]; $column=$parts.Values[1]; $qt=ConvertTo-PgIdentifier $table; $qc=ConvertTo-PgIdentifier $column
        $sql = "select count(*)::text||'|'||count($qc)::text||'|'||coalesce(to_char(min($qc) at time zone 'UTC','YYYY-MM-DD`"T`"HH24:MI:SS.US`"Z`"'),'__NULL__')||'|'||coalesce(to_char(max($qc) at time zone 'UTC','YYYY-MM-DD`"T`"HH24:MI:SS.US`"Z`"'),'__NULL__') from public.$qt;"
        $values = Parse-KeyRows @(Invoke-CoordinatorQuery $Process 'TIME_AGGREGATE' $sql) 4
        $timeBounds["$table.$column"] = [ordered]@{ total="$($values[0].Values[0])"; nonNull="$($values[0].Values[1])"; minUtc=$(if($values[0].Values[2] -eq '__NULL__'){$null}else{"$($values[0].Values[2])"}); maxUtc=$(if($values[0].Values[3] -eq '__NULL__'){$null}else{"$($values[0].Values[3])"}) }
    }

    $securityTables = @('admin_sessions','query_sessions','user_query_code_bind_tokens','user_recovery_emails','recovery_email_verification_codes','query_code_recovery_codes','query_code_recovery_request_events','query_code_recovery_sessions','account_security_audit_logs','admin_auth_audit_events')
    $securityCounts = [ordered]@{}
    foreach($name in $securityTables){ if($counts.Contains($name)){ $securityCounts[$name]=$counts[$name].total } }

    # Preserve every invariant from the schemaVersion=1 preflight baseline as a
    # compatibility layer. Missing future tables (0020/0021) are explicitly zero,
    # while the richer dynamic groups above retain NULL/non-NULL distinctions.
    $legacyCountNames=@('users','admins','orders','payments','products','projects','order_items','cn_merge_logs','import_errors','payment_items','admin_sessions','import_batches','query_sessions','payment_qr_codes','payment_submissions','user_recovery_emails','admin_auth_audit_events','query_code_recovery_codes','account_security_audit_logs','user_query_code_bind_tokens','query_code_recovery_sessions','recovery_email_verification_codes','query_code_recovery_request_events')
    $legacyCounts=[ordered]@{}
    foreach($name in $legacyCountNames){$legacyCounts[$name]=if($counts.Contains($name)){[long]$counts[$name].total}else{[long]0}}
    $sumSpecs=[ordered]@{
        order_items_amount=@('order_items','amount');orders_total_amount=@('orders','total_amount');payments_fee_amount=@('payments','fee_amount');order_items_quantity=@('order_items','quantity');payments_payable_amount=@('payments','payable_amount');import_batches_total_rows=@('import_batches','total_rows');payments_submitted_amount=@('payments','submitted_amount');import_batches_failed_rows=@('import_batches','failed_rows');import_batches_success_rows=@('import_batches','success_rows');payment_items_applied_amount=@('payment_items','applied_amount')
    }
    $legacySums=[ordered]@{};$integerSums=[ordered]@{}
    foreach($name in $sumSpecs.Keys){
        $table=$sumSpecs[$name][0];$column=$sumSpecs[$name][1];$qt=ConvertTo-PgIdentifier $table;$qc=ConvertTo-PgIdentifier $column
        $v=Parse-KeyRows @(Invoke-CoordinatorQuery $Process 'LEGACY_SUM' "select count(*)::text||'|'||count($qc)::text||'|'||coalesce(sum($qc)::text,'0') from public.$qt;") 3
        if($v[0].Values[2] -notmatch '^-?[0-9]+(?:\.[0-9]+)?$'){throw 'Legacy exact sum was not lossless text.'}
        $legacySums[$name]="$($v[0].Values[2])"
        if($column -in @('total_rows','failed_rows','success_rows')){$integerSums["$table.$column"]=[ordered]@{total="$($v[0].Values[0])";nonNull="$($v[0].Values[1])";sum="$($v[0].Values[2])"}}
    }
    $legacyStatus=[ordered]@{}
    $legacyStatusMap=[ordered]@{users='users.status';admins='admins.status';orders='orders.status';payments='payments.status';products='products.status';projects='projects.status';import_batches='import_batches.status';order_items_payment='order_items.payment_status';user_recovery_emails='user_recovery_emails.status';query_code_recovery_codes='query_code_recovery_codes.status';query_code_recovery_sessions='query_code_recovery_sessions.status';recovery_email_verification_codes='recovery_email_verification_codes.status'}
    foreach($name in $legacyStatusMap.Keys){$key=$legacyStatusMap[$name];$legacyStatus[$name]=if($status.Contains($key)){$status[$key]}else{[ordered]@{}}}
    $legacyBinary=[ordered]@{
        payment_qr_codes_image_data=if($binary.Contains('payment_qr_codes.image_data')){[long]$binary['payment_qr_codes.image_data'].nonEmpty}else{[long]0}
        payment_submissions_image_data=if($binary.Contains('payment_submissions.image_data')){[long]$binary['payment_submissions.image_data'].nonEmpty}else{[long]0}
        user_recovery_emails_encrypted_email=if($binary.Contains('user_recovery_emails.encrypted_email')){[long]$binary['user_recovery_emails.encrypted_email'].nonEmpty}else{[long]0}
    }
    $legacyTimes=[ordered]@{}
    foreach($name in @('users','orders','payments','order_items','import_batches')){$v=$timeBounds["$name.created_at"];$legacyTimes[$name]=@($v.minUtc,$v.maxUtc)}

    $sourceFacts=Parse-KeyRows @(Invoke-CoordinatorQuery $Process 'SOURCE_METADATA' "select pg_database_size(current_database())::text||'|'||(select count(*) from pg_stat_activity where datname=current_database())::text||'|'||(select case when exists(select 1 from pg_extension where extname='pgcrypto') then '1' else '0' end);") 3

    return [ordered]@{
        schemaVersion = 2
        sourceDatabase = $SourceDatabase
        serverVersion = $ServerVersion
        snapshotStrategy = 'pg_export_snapshot + pg_dump --snapshot'
        snapshotCoordinatorRunId = $RunId
        snapshotExportedAtUtc = $ExportedAtUtc
        migrationSummary = [ordered]@{ count=[int]$Migrations.Count; max=$Migrations[-1] }
        counts = $counts
        statusDistributions = $status
        binaryNonemptyCounts = $binary
        numericSums = $numeric
        integerSums = $integerSums
        utcTimeBounds = $timeBounds
        securityAuxiliaryCounts = $securityCounts
        databaseSizeBytes = "$($sourceFacts[0].Values[0])"
        activeConnectionsAtCapture = "$($sourceFacts[0].Values[1])"
        pgcryptoExists = ($sourceFacts[0].Values[2] -eq '1')
        businessInvariants = [ordered]@{ sums=$legacySums;counts=$legacyCounts;time_bounds_utc=$legacyTimes;status_distributions=$legacyStatus;binary_nonempty_counts=$legacyBinary }
    }
}

function Get-GitRevision {
    $git = (Get-Command git.exe -ErrorAction SilentlyContinue).Source
    if (-not $git) { $git = (Get-Command git -ErrorAction Stop).Source }
    $revision = Invoke-CheckedNative $git @('-C',$repoRoot,'rev-parse','HEAD') 'git revision'
    if ($revision -notmatch '^[0-9a-f]{40}$') { throw 'Git revision was not a full SHA.' }
    return $revision
}

function Get-SafeFailureSummary([System.Exception]$Exception) {
    $summary = if ($Exception -and $Exception.Message) { $Exception.Message } else { 'Unspecified snapshot publication failure.' }
    $summary = $summary -replace '[\r\n]+',' '
    $summary = $summary -replace '(?i)(postgres(?:ql)?://)[^\s]+','$1[REDACTED]'
    $summary = $summary -replace '(?i)((?:DATABASE_PASSWORD|PGPASSWORD|SMTP_PASSWORD)\s*=\s*)[^\s]+','$1[REDACTED]'
    if ($summary.Length -gt 300) { $summary = $summary.Substring(0,300) }
    return $summary
}

function Move-PrepublishValidationAside([string]$Directory) {
    $validationPath = Join-Path $Directory 'validation.json'
    if (-not (Test-Path -LiteralPath $validationPath -PathType Leaf)) { return }
    $evidencePath = Join-Path $Directory 'validation.prepublish-pass.json'
    if (Test-Path -LiteralPath $evidencePath) {
        $evidencePath = Join-Path $Directory ('validation.prepublish-pass-' + [Guid]::NewGuid().ToString('N') + '.json')
    }
    Move-Item -LiteralPath $validationPath -Destination $evidencePath
}

function Write-FailureMarker {
    param(
        [string]$Directory,
        [string]$Stage,
        [System.Exception]$Exception,
        [string]$OriginalFinalPath,
        [string]$FailedPath
    )
    if (-not $Directory -or -not (Test-Path -LiteralPath $Directory -PathType Container)) { return }
    Move-PrepublishValidationAside $Directory
    $path = Join-Path $Directory 'validation.failed.json'
    $partialPath = $path + '.partial'
    $model = [ordered]@{
        schemaVersion=1; verdict='FAIL'; publicationState='failed'; failedStage=$Stage
        snapshotCoordinatorRunId=$runId; originalFinalPath=$OriginalFinalPath; failedEvidencePath=$FailedPath
        failureSummary=(Get-SafeFailureSummary $Exception); failedAtUtc=[DateTime]::UtcNow.ToString('o')
        successArtifactsPublished=$false; productionRestoreAllowed=$false
    }
    Write-Utf8NoBom $partialPath (Get-CanonicalJson $model)
    Move-Item -LiteralPath $partialPath -Destination $path
}

function Write-FailureEvidence([string]$Stage, [System.Exception]$Exception) {
    if ($stagingDirectory -and (Test-Path -LiteralPath $stagingDirectory -PathType Container)) {
        Write-FailureMarker $stagingDirectory $Stage $Exception $finalDirectory $stagingDirectory
    }
}

function Assert-ArtifactPublicationState {
    param(
        [string]$Directory,
        [string]$ExpectedPublicationState,
        [string]$ExpectedRunId,
        [int]$ExpectedTocCount
    )
    Assert-StagingArtifactSet $Directory $baseName
    $dumpPath = Join-Path $Directory ($baseName + '.dump')
    $baselinePath = Join-Path $Directory 'source-baseline.json'
    $migrationsPath = Join-Path $Directory 'source-migrations.txt'
    $metadataPath = Join-Path $Directory 'export-metadata.json'
    $validationPath = Join-Path $Directory 'validation.json'
    $baselineModel = Get-Content -LiteralPath $baselinePath -Raw | ConvertFrom-Json
    $metadataModel = Get-Content -LiteralPath $metadataPath -Raw | ConvertFrom-Json
    $validationModel = Get-Content -LiteralPath $validationPath -Raw | ConvertFrom-Json
    $actualDumpHash = (Get-FileHash -LiteralPath $dumpPath -Algorithm SHA256).Hash.ToUpperInvariant()
    $actualBaselineHash = (Get-FileHash -LiteralPath $baselinePath -Algorithm SHA256).Hash.ToUpperInvariant()
    $actualMigrationsHash = (Get-FileHash -LiteralPath $migrationsPath -Algorithm SHA256).Hash.ToUpperInvariant()
    $actualDumpSize = (Get-Item -LiteralPath $dumpPath).Length
    $migrationLines = @(Get-Content -LiteralPath $migrationsPath | Where-Object { $_ -ne '' })
    if ($validationModel.publicationState -cne $ExpectedPublicationState) { throw "Validation publication state is not $ExpectedPublicationState." }
    if ($validationModel.verdict -cne 'PASS' -or $validationModel.productionRestoreAllowed -ne $false) { throw 'Validation verdict or restore boundary is invalid.' }
    if ($validationModel.snapshotConsistency.coordinatorRunId -cne $ExpectedRunId -or $baselineModel.snapshotCoordinatorRunId -cne $ExpectedRunId -or $metadataModel.snapshotCoordinatorRunId -cne $ExpectedRunId) { throw 'Snapshot coordinator run binding differs across artifacts.' }
    if ($validationModel.sha256.value -cne $actualDumpHash -or $metadataModel.dumpSha256 -cne $actualDumpHash -or $baselineModel.dumpSha256 -cne $actualDumpHash) { throw 'Dump SHA-256 binding differs across artifacts.' }
    if ([long]$metadataModel.dumpSizeBytes -ne $actualDumpSize -or [long]$baselineModel.dumpSizeBytes -ne $actualDumpSize) { throw 'Dump size binding differs across artifacts.' }
    if ($metadataModel.baselineSha256 -cne $actualBaselineHash -or $metadataModel.migrationsSha256 -cne $actualMigrationsHash) { throw 'Baseline or migration SHA-256 binding is invalid.' }
    if ([int]$metadataModel.tocCount -ne $ExpectedTocCount -or [int]$validationModel.pgRestoreList.tocCount -ne $ExpectedTocCount) { throw 'TOC binding differs across artifacts.' }
    if ([int]$metadataModel.schemaMigrationCount -ne $migrationLines.Count -or [int]$validationModel.migrationsCompleteSet.count -ne $migrationLines.Count -or $metadataModel.schemaMigrationMax -cne $migrationLines[-1]) { throw 'Migration set binding differs across artifacts.' }
}

function Invoke-PostRenameTestMutation([string]$Stage) {
    if ($env:PJSK_SNAPSHOT_TEST_FAIL_STAGE -cne $Stage) { return }
    switch ($Stage) {
        'post-rename-final-missing' { throw 'Injected post-rename final-directory observation failure.' }
        'post-rename-staging-remains' {
            New-Item -ItemType Directory -Path $stagingDirectory | Out-Null
            Write-Utf8NoBom (Join-Path $stagingDirectory 'unknown-staging-evidence.txt') 'preserve unknown post-rename staging evidence'
        }
        'post-rename-extra-file' { Write-Utf8NoBom (Join-Path $finalDirectory 'unexpected.txt') 'preserve unexpected post-rename artifact' }
    }
}

function Invoke-PostRenameFailureQuarantine([System.Exception]$Exception, [string]$Stage) {
    $failedDirectory = $finalDirectory + '.failed-' + [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssfffZ') + '-' + [Guid]::NewGuid().ToString('N').Substring(0,8)
    try {
        if (-not (Test-Path -LiteralPath $finalDirectory -PathType Container)) { throw "Final directory is missing after its rename: $finalDirectory" }
        Assert-NewOutputDirectory $failedDirectory
        if ([IO.Path]::GetFullPath([IO.Path]::GetDirectoryName($failedDirectory)) -cne [IO.Path]::GetFullPath([IO.Path]::GetDirectoryName($finalDirectory)) -or [IO.Path]::GetPathRoot($failedDirectory) -cne [IO.Path]::GetPathRoot($finalDirectory)) { throw 'Failed evidence path is not beside the final directory on the same volume.' }
        if ($env:PJSK_SNAPSHOT_TEST_FAIL_QUARANTINE -eq '1') { throw 'Injected quarantine directory rename failure.' }
        Move-Item -LiteralPath $finalDirectory -Destination $failedDirectory
        Write-FailureMarker $failedDirectory 'post-rename-validation' $Exception $finalDirectory $failedDirectory
        if ((Test-Path -LiteralPath $finalDirectory) -or -not (Test-Path -LiteralPath $failedDirectory -PathType Container)) { throw 'Quarantine rename did not reach the expected filesystem state.' }
        if (Test-Path -LiteralPath (Join-Path $failedDirectory 'validation.json')) { throw 'Quarantined evidence still exposes validation.json.' }
        return $failedDirectory
    } catch {
        $quarantineException = $_.Exception
        $markerStatus = 'not written'
        $evidenceDirectory = if (Test-Path -LiteralPath $failedDirectory -PathType Container) { $failedDirectory } elseif (Test-Path -LiteralPath $finalDirectory -PathType Container) { $finalDirectory } else { $null }
        if ($evidenceDirectory) {
            try {
                Write-FailureMarker $evidenceDirectory 'post-rename-validation' $Exception $finalDirectory $evidenceDirectory
                $markerStatus = "written inside '$evidenceDirectory'"
            } catch {
                $markerStatus = 'could not be written'
            }
        }
        $locationStatus = if (Test-Path -LiteralPath $finalDirectory) { "the final path still exists at '$finalDirectory'" } elseif (Test-Path -LiteralPath $failedDirectory) { "the failed evidence path exists at '$failedDirectory'" } else { 'neither expected evidence path could be confirmed' }
        throw "Post-rename publication failed; quarantine failed or could not be fully verified; $locationStatus. Manual isolation is required. Failure marker $markerStatus. Quarantine error: $(Get-SafeFailureSummary $quarantineException)"
    }
}

try {
    Assert-SnapshotHost $SourceHost
    Assert-SnapshotIdentifier $SourceDatabase 'SourceDatabase'
    Assert-SnapshotIdentifier $SourceUser 'SourceUser'
    Assert-SnapshotMode -Mode $Mode -WriteFreezeConfirmed:$WriteFreezeConfirmed -MaintenanceWindowId $MaintenanceWindowId
    $normalizedOutputRoot = Resolve-SnapshotOutputRoot $OutputRoot $repoRoot $Mode

    $psql = Join-Path $PgBinDirectory 'psql.exe'
    $pgDump = Join-Path $PgBinDirectory 'pg_dump.exe'
    $pgRestore = Join-Path $PgBinDirectory 'pg_restore.exe'
    foreach($tool in @($psql,$pgDump,$pgRestore)){ if(-not(Test-Path -LiteralPath $tool -PathType Leaf)){ throw "Required PostgreSQL tool not found: $tool" } }

    $psqlVersion = Get-PostgresVersionInfo (Invoke-CheckedNative $psql @('--version') 'psql version') 'psql'
    $dumpVersion = Get-PostgresVersionInfo (Invoke-CheckedNative $pgDump @('--version') 'pg_dump version') 'pg_dump'
    $restoreVersion = Get-PostgresVersionInfo (Invoke-CheckedNative $pgRestore @('--version') 'pg_restore version') 'pg_restore'
    if ($psqlVersion.Version -ne $dumpVersion.Version -or $dumpVersion.Version -ne $restoreVersion.Version) { throw 'psql, pg_dump, and pg_restore versions must match exactly.' }

    $activePassfile = $Passfile
    $authMode = 'explicit-passfile'
    if (-not $activePassfile) {
        $activePassfile = Join-Path $env:APPDATA 'postgresql\pgpass.conf'
        $authMode = 'default-pgpass'
    }
    $aclReason = ''
    if (-not (Test-PassfileAcl $activePassfile ([ref]$aclReason))) { throw "Safe PostgreSQL authentication is unavailable: $aclReason" }

    if (-not (Test-Path -LiteralPath $normalizedOutputRoot)) { New-Item -ItemType Directory -Path $normalizedOutputRoot | Out-Null }
    $utc = [DateTime]::UtcNow
    $stamp = $utc.ToString('yyyyMMddTHHmmssfffZ')
    $baseName = if($Mode -eq 'rehearsal'){ "pjsk-rehearsal-$stamp" }else{ "pjsk-cutover-final-$MaintenanceWindowId-$stamp" }
    Assert-ArtifactBaseName $Mode $baseName
    $finalDirectory = Join-Path $normalizedOutputRoot $baseName
    $stagingDirectory = Join-Path $normalizedOutputRoot ('.pjsk-snapshot-staging-' + [Guid]::NewGuid().ToString('N'))
    Assert-AtomicPublicationPaths $stagingDirectory $finalDirectory
    Assert-NewOutputDirectory $finalDirectory
    Assert-NewOutputDirectory $stagingDirectory
    New-Item -ItemType Directory -Path $stagingDirectory | Out-Null

    $dumpFinal = Join-Path $stagingDirectory ($baseName + '.dump')
    $dumpPartial = $dumpFinal + '.partial'
    $baselinePartial = Join-Path $stagingDirectory 'source-baseline.json.partial'
    $migrationsPartial = Join-Path $stagingDirectory 'source-migrations.txt.partial'
    $metadataPartial = Join-Path $stagingDirectory 'export-metadata.json.partial'
    $validationPartial = Join-Path $stagingDirectory 'validation.json.partial'
    $shaPartial = $dumpFinal + '.sha256.partial'

    $failureStage = 'coordinator-start'
    $coordinator = New-PsqlCoordinator $psql $activePassfile
    $null = Invoke-CoordinatorQuery $coordinator 'BEGIN' 'begin transaction isolation level repeatable read read only;'
    $transactionOpen = $true
    $facts = @(Invoke-CoordinatorQuery $coordinator 'SERVER_FACTS' "select current_database()||'|'||current_setting('server_version')||'|'||current_setting('server_version_num')||'|'||current_setting('transaction_isolation')||'|'||current_setting('transaction_read_only');")
    if ($facts.Count -ne 1) { throw 'Could not read source server facts.' }
    $factParts = @($facts[0] -split '\|',5)
    if ($factParts.Count -ne 5 -or $factParts[0] -ne $SourceDatabase -or $factParts[3] -ne 'repeatable read' -or $factParts[4] -ne 'on') { throw 'Coordinator transaction does not match the requested read-only repeatable-read source.' }
    $serverVersion = $factParts[1]
    Assert-PostgresServerVersion $serverVersion $factParts[2]

    $failureStage = 'snapshot-export'
    $snapshot = @(Invoke-CoordinatorQuery $coordinator 'EXPORT_SNAPSHOT' 'select pg_export_snapshot();')
    if ($snapshot.Count -ne 1 -or $snapshot[0] -notmatch '^[0-9A-Fa-f:-]+$') { throw 'pg_export_snapshot returned an invalid snapshot identifier.' }
    $snapshotId = $snapshot[0]
    $runId = [Guid]::NewGuid().ToString('D')
    $exportedAt = @(Invoke-CoordinatorQuery $coordinator 'EXPORT_TIME' "select to_char(clock_timestamp() at time zone 'UTC','YYYY-MM-DD`"T`"HH24:MI:SS.US`"Z`"');")
    if ($exportedAt.Count -ne 1) { throw 'Could not record snapshot export time.' }

    $failureStage = 'migrations'
    $migrations = @(Invoke-CoordinatorQuery $coordinator 'MIGRATIONS' 'select version from public.schema_migrations order by version collate "C";')
    $migrationReason = ''
    if (-not (Test-MigrationVersionSet $migrations ([ref]$migrationReason))) { throw $migrationReason }
    Write-Utf8NoBom $migrationsPartial (($migrations -join "`n") + "`n")
    Invoke-TestFailureInjection 'migrations-written'

    $failureStage = 'baseline'
    $baseline = Get-AggregateBaseline $coordinator $runId $exportedAt[0] $serverVersion $migrations
    Invoke-TestFailureInjection 'baseline-built'

    $failureStage = 'pg-dump'
    $oldPassfile = $env:PGPASSFILE
    try {
        $env:PGPASSFILE = $activePassfile
        $dumpArgs = @('--format=custom','--no-owner','--no-privileges','-w','--host',$SourceHost,'--port',"$SourcePort",'--username',$SourceUser,"--snapshot=$snapshotId",'--file',$dumpPartial,$SourceDatabase)
        $null = Invoke-CheckedNative $pgDump $dumpArgs 'pg_dump'
    } finally {
        if ($null -eq $oldPassfile) { Remove-Item Env:\PGPASSFILE -ErrorAction SilentlyContinue } else { $env:PGPASSFILE=$oldPassfile }
    }
    if (-not (Test-Path -LiteralPath $dumpPartial -PathType Leaf) -or (Get-Item -LiteralPath $dumpPartial).Length -le 0) { throw 'pg_dump produced an empty or missing partial archive.' }

    $failureStage = 'pg-restore-list'
    $toc = @(Invoke-CheckedNative $pgRestore @('--list',$dumpPartial) 'pg_restore --list' -ReturnLines)
    $tocCount = @($toc | Where-Object { $_ -match '^\d+;' }).Count
    if ($tocCount -le 0) { throw 'pg_restore --list returned no TOC entries.' }
    $dumpHash = (Get-FileHash -LiteralPath $dumpPartial -Algorithm SHA256).Hash.ToUpperInvariant()
    $dumpSize = (Get-Item -LiteralPath $dumpPartial).Length
    $gitRevision = Get-GitRevision
    $baseline.dumpCompletedAtUtc=[DateTime]::UtcNow.ToString('o')
    $baseline.dumpFileName=[IO.Path]::GetFileName($dumpFinal)
    $baseline.dumpSizeBytes=$dumpSize
    $baseline.dumpSha256=$dumpHash
    $baseline.pgDumpVersion=$dumpVersion.Version
    $baseline.gitReleaseSha=$gitRevision
    $baseline.migrationListFileName='source-migrations.txt'
    Write-Utf8NoBom $baselinePartial (Get-CanonicalJson $baseline)
    Invoke-TestFailureInjection 'baseline-written'
    $alive = @(Invoke-CoordinatorQuery $coordinator 'SNAPSHOT_ALIVE' "select case when current_setting('transaction_isolation')='repeatable read' and current_setting('transaction_read_only')='on' then '1' else '0' end;")
    if ($alive.Count -ne 1 -or $alive[0] -ne '1') { throw 'Coordinator transaction closed before dump verification.' }

    $null = Invoke-CoordinatorQuery $coordinator 'COMMIT' 'commit;'
    $transactionOpen = $false
    $coordinator.StandardInput.WriteLine('\q')
    $coordinator.StandardInput.Close()
    $coordinator.WaitForExit()
    if ($coordinator.ExitCode -ne 0) { throw "Coordinator psql exited with code $($coordinator.ExitCode)." }
    $coordinator.Dispose()
    $coordinator=$null

    $failureStage = 'artifact-validation'
    $baselineHash = (Get-FileHash -LiteralPath $baselinePartial -Algorithm SHA256).Hash.ToUpperInvariant()
    $migrationHash = (Get-FileHash -LiteralPath $migrationsPartial -Algorithm SHA256).Hash.ToUpperInvariant()
    $endUtc = [DateTime]::UtcNow
    $durationMs = [long]($endUtc-$utc).TotalMilliseconds

    $metadata = [ordered]@{
        schemaVersion=1; mode=$Mode; productionRestoreAllowed=$false
        databaseName=$SourceDatabase; serverVersion=$serverVersion
        pgDumpVersion=$dumpVersion.Version; pgRestoreVersion=$restoreVersion.Version
        startedAtUtc=$utc.ToString('o'); endedAtUtc=$endUtc.ToString('o'); durationMilliseconds=$durationMs
        snapshotStrategy='pg_export_snapshot + coordinator transaction + pg_dump --snapshot'
        snapshotCoordinatorRunId=$runId; dumpFileName=[IO.Path]::GetFileName($dumpFinal)
        dumpSizeBytes=$dumpSize; dumpSha256=$dumpHash; tocCount=$tocCount
        baselineSha256=$baselineHash; migrationsSha256=$migrationHash
        gitCommit=$gitRevision; schemaMigrationCount=$migrations.Count; schemaMigrationMax=$migrations[-1]
        authenticationMode=$authMode
    }
    Write-Utf8NoBom $metadataPartial (Get-CanonicalJson $metadata)
    Write-Utf8NoBom $shaPartial ("$dumpHash  $([IO.Path]::GetFileName($dumpFinal))`n")
    Invoke-TestFailureInjection 'metadata-written'
    if (-not (Test-ExpectedSha256 $dumpPartial $dumpHash)) { throw 'Dump SHA-256 readback verification failed.' }

    $scanReason=''
    $textToScan = ([IO.File]::ReadAllText($baselinePartial,$utf8NoBom) + "`n" + [IO.File]::ReadAllText($migrationsPartial,$utf8NoBom) + "`n" + [IO.File]::ReadAllText($metadataPartial,$utf8NoBom))
    $scanPassed = Test-SnapshotSensitiveText $textToScan ([ref]$scanReason)
    if (-not $scanPassed) { throw "Sensitive-information scan failed ($scanReason)." }
    $dumpAscii = [Text.Encoding]::ASCII.GetString([IO.File]::ReadAllBytes($dumpPartial))
    if (-not (Test-SnapshotSensitiveText $dumpAscii ([ref]$scanReason))) { throw "Archive sensitive-information scan failed ($scanReason)." }

    $validation = [ordered]@{
        schemaVersion=1; verdict='PASS'; publicationState='staged'; productionRestoreAllowed=$false
        sha256=[ordered]@{ passed=$true; value=$dumpHash }
        pgRestoreList=[ordered]@{ passed=$true; tocCount=$tocCount }
        snapshotConsistency=[ordered]@{ passed=$true; strategy='exported snapshot'; coordinatorRunId=$runId; baselineHashBound=$true; migrationsHashBound=$true; dumpSnapshotArgument=$true }
        baselineInternalConsistency=[ordered]@{ passed=$true; exactNumericStrings=$true; utcTimestamps=$true; migrationSummaryCrossCheck=$true }
        migrationsCompleteSet=[ordered]@{ passed=$true; count=$migrations.Count; max=$migrations[-1]; duplicateCount=0 }
        partialFiles=[ordered]@{ passed=$true; publishedPartialCount=0 }
        sensitiveInformationScan=[ordered]@{ passed=$true; credentialMarkerMatches=0 }
    }
    Write-Utf8NoBom $validationPartial (Get-CanonicalJson $validation)
    Invoke-TestFailureInjection 'validation-written'

    $failureStage = 'staging-finalization'
    Move-Item -LiteralPath $dumpPartial -Destination $dumpFinal
    Move-Item -LiteralPath $shaPartial -Destination ($dumpFinal+'.sha256')
    Move-Item -LiteralPath $baselinePartial -Destination (Join-Path $stagingDirectory 'source-baseline.json')
    Move-Item -LiteralPath $migrationsPartial -Destination (Join-Path $stagingDirectory 'source-migrations.txt')
    Move-Item -LiteralPath $metadataPartial -Destination (Join-Path $stagingDirectory 'export-metadata.json')
    Move-Item -LiteralPath $validationPartial -Destination (Join-Path $stagingDirectory 'validation.json')
    Invoke-TestFailureInjection 'staging-finalized'

    $failureStage = 'pre-publication-verification'
    Assert-ArtifactPublicationState $stagingDirectory 'staged' $runId $tocCount
    $finalToc = @(Invoke-CheckedNative $pgRestore @('--list',$dumpFinal) 'final staging pg_restore --list' -ReturnLines)
    if (@($finalToc | Where-Object { $_ -match '^\d+;' }).Count -ne $tocCount) { throw 'Final staging TOC differs from the previously verified archive.' }
    Invoke-TestFailureInjection 'pre-publication'
    Assert-NewOutputDirectory $finalDirectory
    Assert-AtomicPublicationPaths $stagingDirectory $finalDirectory

    $failureStage = 'directory-publication'
    Invoke-TestFailureInjection 'directory-rename'
    Move-Item -LiteralPath $stagingDirectory -Destination $finalDirectory
    $directoryRenameCompleted = $true

    $failureStage = 'post-rename-validation'
    Invoke-PostRenameTestMutation 'post-rename-final-missing'
    if (-not (Test-Path -LiteralPath $finalDirectory -PathType Container)) { throw 'Final directory is missing after directory rename.' }
    Invoke-PostRenameTestMutation 'post-rename-staging-remains'
    if (Test-Path -LiteralPath $stagingDirectory) { throw 'Staging path still exists after directory rename.' }
    Invoke-PostRenameTestMutation 'post-rename-extra-file'
    Assert-ArtifactPublicationState $finalDirectory 'staged' $runId $tocCount
    $publishedToc = @(Invoke-CheckedNative $pgRestore @('--list',(Join-Path $finalDirectory ($baseName+'.dump'))) 'post-rename pg_restore --list' -ReturnLines)
    if (@($publishedToc | Where-Object { $_ -match '^\d+;' }).Count -ne $tocCount) { throw 'Post-rename TOC differs from the verified staging archive.' }

    $failureStage = 'validation-publication'
    $publishedValidation = [ordered]@{}
    foreach ($key in $validation.Keys) { $publishedValidation[$key] = $validation[$key] }
    $publishedValidation.publicationState = 'published'
    $publishedValidationPartial = Join-Path $finalDirectory 'validation.json.publish.partial'
    $stagedValidationBackup = Join-Path $finalDirectory 'validation.staged-replace-backup.json'
    Write-Utf8NoBom $publishedValidationPartial (Get-CanonicalJson $publishedValidation)
    Invoke-TestFailureInjection 'validation-publish-update'
    [System.IO.File]::Replace($publishedValidationPartial,(Join-Path $finalDirectory 'validation.json'),$stagedValidationBackup)
    Remove-Item -LiteralPath $stagedValidationBackup -Force

    $failureStage = 'published-final-verification'
    Assert-ArtifactPublicationState $finalDirectory 'published' $runId $tocCount
    $finalPublishedToc = @(Invoke-CheckedNative $pgRestore @('--list',(Join-Path $finalDirectory ($baseName+'.dump'))) 'published final pg_restore --list' -ReturnLines)
    if (@($finalPublishedToc | Where-Object { $_ -match '^\d+;' }).Count -ne $tocCount) { throw 'Published final TOC differs from the verified archive.' }
    if (Test-Path -LiteralPath $stagingDirectory) { throw 'Staging path reappeared before publication completed.' }
    $published = $true
    try {
        Write-Output "Snapshot export PASS"
        Write-Output "  mode      : $Mode"
        Write-Output "  directory : $finalDirectory"
        Write-Output "  dump      : $([IO.Path]::GetFileName($dumpFinal))"
        Write-Output "  size      : $dumpSize bytes"
        Write-Output "  sha256    : $dumpHash"
        Write-Output "  TOC       : $tocCount"
    } catch {}
    exit 0
} catch {
    $originalException = $_.Exception
    if ($transactionOpen -and $coordinator -and -not $coordinator.HasExited) {
        try { $null=Invoke-CoordinatorQuery $coordinator 'ROLLBACK' 'rollback;'; $transactionOpen=$false } catch {}
    }
    if ($coordinator -and -not $coordinator.HasExited) {
        try { $coordinator.StandardInput.WriteLine('\q'); $coordinator.StandardInput.Close(); $coordinator.WaitForExit(3000) | Out-Null } catch {}
    }
    if($coordinator){try{$coordinator.Dispose()}catch{}}
    $reportException = $originalException
    if (-not $published -and $directoryRenameCompleted) {
        try {
            $quarantinedPath = Invoke-PostRenameFailureQuarantine $originalException $failureStage
            $reportException = New-Object System.Exception ("Post-rename publication failed and was quarantined at '$quarantinedPath'. " + (Get-SafeFailureSummary $originalException))
        } catch {
            $reportException = $_.Exception
        }
    } elseif (-not $published) {
        try { Write-FailureEvidence $failureStage $originalException } catch {}
    }
    Write-Error ("Snapshot export failed at {0}: {1}" -f $failureStage,(Get-SafeFailureSummary $reportException))
    exit 1
}
