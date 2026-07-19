# Compare an isolated restored database with a schemaVersion=2 aggregate baseline.
# Only aggregate counts and PASS/FAIL are printed; no business row is returned.
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$RestoredDatabase,
    [Parameter(Mandatory)][string]$BaselineFile,
    [Parameter(Mandatory)][string]$MigrationsFile,
    [string]$PostgresBin='D:\PostgreSQL\18\bin',
    [string]$HostName='127.0.0.1',
    [ValidateRange(1,65535)][int]$Port=5432,
    [string]$Username='postgres',
    [string]$ExpectedOwner='postgres',
    [string]$Passfile
)

$ErrorActionPreference='Stop'
Set-StrictMode -Version 2.0
. (Join-Path $PSScriptRoot '_SnapshotCoordinator.ps1')

function Fail([string]$Message){Write-Error $Message;exit 1}
if($RestoredDatabase -notmatch '^pjsk_restore_test_[a-z0-9_]+$'){Fail 'RestoredDatabase must use the isolated pjsk_restore_test_* prefix.'}
Assert-SnapshotHost $HostName
Assert-SnapshotIdentifier $RestoredDatabase 'RestoredDatabase'
Assert-SnapshotIdentifier $Username 'Username'
Assert-SnapshotIdentifier $ExpectedOwner 'ExpectedOwner'
foreach($file in @($BaselineFile,$MigrationsFile)){if(-not(Test-Path -LiteralPath $file -PathType Leaf)){Fail "Required input file not found: $file"}}
$psql=Join-Path $PostgresBin 'psql.exe'
if(-not(Test-Path -LiteralPath $psql -PathType Leaf)){Fail "PostgreSQL tool not found: $psql"}
$version=& $psql --version
if($LASTEXITCODE -ne 0){Fail 'psql --version failed.'}
try{$null=Get-PostgresVersionInfo ($version -join ' ') 'psql'}catch{Fail $_.Exception.Message}

$activePassfile=$Passfile
if(-not $activePassfile){$activePassfile=Join-Path $env:APPDATA 'postgresql\pgpass.conf'}
$aclReason=''
if(-not(Test-PassfileAcl $activePassfile ([ref]$aclReason))){Fail "Safe PostgreSQL authentication is unavailable: $aclReason"}

try{$baseline=Get-Content -LiteralPath $BaselineFile -Raw | ConvertFrom-Json}catch{Fail 'Baseline JSON is invalid.'}
if([int]$baseline.schemaVersion -ne 2){Fail 'Baseline schemaVersion must be 2.'}
$expectedMigrations=@(Get-Content -LiteralPath $MigrationsFile | ForEach-Object{"$_".Trim()} | Where-Object{$_})
$migrationReason=''
if(-not(Test-MigrationVersionSet $expectedMigrations ([ref]$migrationReason))){Fail $migrationReason}

$connection=@('-X','-w','--host',$HostName,'--port',"$Port",'--username',$Username,'--dbname',$RestoredDatabase,'--no-align','--tuples-only','--set','ON_ERROR_STOP=on')
function Invoke-Rows([string]$Sql){
    $old=$env:PGPASSFILE
    try{$env:PGPASSFILE=$activePassfile;$out=@(& $psql @connection --command $Sql 2>&1 | ForEach-Object{"$_"});$code=$LASTEXITCODE}
    finally{if($null -eq $old){Remove-Item Env:\PGPASSFILE -ErrorAction SilentlyContinue}else{$env:PGPASSFILE=$old}}
    if($code -ne 0){Fail 'A baseline verification query failed.'}
    return @($out|ForEach-Object{"$_".Trim()}|Where-Object{$_})
}
function Invoke-One([string]$Sql){$rows=@(Invoke-Rows $Sql);if($rows.Count -ne 1){Fail 'A scalar verification query returned an unexpected shape.'};return $rows[0]}
function Split-Key([string]$Key){
    if($Key -notmatch '^([A-Za-z_][A-Za-z0-9_]*)\.([A-Za-z_][A-Za-z0-9_]*)$'){Fail 'Baseline contains an unsafe aggregate key.'}
    return @($matches[1],$matches[2])
}
function Assert-Equal([string]$Actual,[string]$Expected,[string]$Category){if($Actual -cne $Expected){Fail "$Category aggregate mismatch."}}

$server=Invoke-One "select current_setting('server_version')||'|'||current_database();"
Assert-Equal $server ("$($baseline.serverVersion)|$RestoredDatabase") 'server/database'
$actualMigrations=@(Invoke-Rows 'select version from public.schema_migrations order by version;' | Sort-Object -CaseSensitive)
if(-not(Test-MigrationSetExactMatch $expectedMigrations $actualMigrations)){Fail 'Restored migration set differs from source-migrations.txt.'}
if([int]$baseline.migrationSummary.count -ne $actualMigrations.Count -or "$($baseline.migrationSummary.max)" -cne $actualMigrations[-1]){Fail 'Baseline migration summary does not match the restored full set.'}

$countChecked=0
foreach($p in $baseline.counts.PSObject.Properties){
    $qt=ConvertTo-PgIdentifier $p.Name
    Assert-Equal (Invoke-One "select count(*)::text from public.$qt;") "$($p.Value.total)" 'table count'
    $countChecked++
}
$statusChecked=0
foreach($p in $baseline.statusDistributions.PSObject.Properties){
    $key=Split-Key $p.Name;$qt=ConvertTo-PgIdentifier $key[0];$qc=ConvertTo-PgIdentifier $key[1]
    $actual=[ordered]@{}
    foreach($line in @(Invoke-Rows "select coalesce($qc,'__NULL__')||'|'||count(*)::text from public.$qt group by $qc;" | Sort-Object -CaseSensitive)){$a=@($line -split '\|',2);if($a.Count-ne 2){Fail 'Status aggregate shape mismatch.'};$actual[$a[0]]=$a[1]}
    $expected=[ordered]@{};foreach($e in $p.Value.PSObject.Properties){$expected[$e.Name]="$($e.Value)"}
    Assert-Equal (Get-CanonicalJson $actual) (Get-CanonicalJson $expected) 'status distribution';$statusChecked++
}
$binaryChecked=0
foreach($p in $baseline.binaryNonemptyCounts.PSObject.Properties){
    $key=Split-Key $p.Name;$qt=ConvertTo-PgIdentifier $key[0];$qc=ConvertTo-PgIdentifier $key[1]
    $actual=Invoke-One "select count(*)::text||'|'||count($qc)::text||'|'||count(*) filter(where $qc is not null and octet_length($qc)>0)::text from public.$qt;"
    Assert-Equal $actual ("$($p.Value.total)|$($p.Value.nonNull)|$($p.Value.nonEmpty)") 'binary nonempty';$binaryChecked++
}
$numericChecked=0
foreach($p in $baseline.numericSums.PSObject.Properties){
    $key=Split-Key $p.Name;$qt=ConvertTo-PgIdentifier $key[0];$qc=ConvertTo-PgIdentifier $key[1]
    $actual=Invoke-One "select count(*)::text||'|'||count($qc)::text||'|'||coalesce(sum($qc)::text,'__NULL__') from public.$qt;"
    $sum=if($null -eq $p.Value.sum){'__NULL__'}else{"$($p.Value.sum)"}
    Assert-Equal $actual ("$($p.Value.total)|$($p.Value.nonNull)|$sum") 'numeric sum';$numericChecked++
}
$integerChecked=0
foreach($p in $baseline.integerSums.PSObject.Properties){
    $key=Split-Key $p.Name;$qt=ConvertTo-PgIdentifier $key[0];$qc=ConvertTo-PgIdentifier $key[1]
    $actual=Invoke-One "select count(*)::text||'|'||count($qc)::text||'|'||coalesce(sum($qc)::text,'0') from public.$qt;"
    Assert-Equal $actual ("$($p.Value.total)|$($p.Value.nonNull)|$($p.Value.sum)") 'integer sum';$integerChecked++
}
$timeChecked=0
foreach($p in $baseline.utcTimeBounds.PSObject.Properties){
    $key=Split-Key $p.Name;$qt=ConvertTo-PgIdentifier $key[0];$qc=ConvertTo-PgIdentifier $key[1]
    $sql="select count(*)::text||'|'||count($qc)::text||'|'||coalesce(to_char(min($qc) at time zone 'UTC','YYYY-MM-DD')||'T'||to_char(min($qc) at time zone 'UTC','HH24:MI:SS.US')||'Z','__NULL__')||'|'||coalesce(to_char(max($qc) at time zone 'UTC','YYYY-MM-DD')||'T'||to_char(max($qc) at time zone 'UTC','HH24:MI:SS.US')||'Z','__NULL__') from public.$qt;"
    $min=if($null -eq $p.Value.minUtc){'__NULL__'}else{"$($p.Value.minUtc)"};$max=if($null -eq $p.Value.maxUtc){'__NULL__'}else{"$($p.Value.maxUtc)"}
    Assert-Equal (Invoke-One $sql) ("$($p.Value.total)|$($p.Value.nonNull)|$min|$max") 'UTC time bound';$timeChecked++
}
foreach($p in $baseline.securityAuxiliaryCounts.PSObject.Properties){if(-not $baseline.counts.PSObject.Properties[$p.Name]){Fail 'Security auxiliary count lacks its table-count binding.'};Assert-Equal "$($p.Value)" "$($baseline.counts.PSObject.Properties[$p.Name].Value.total)" 'security auxiliary'}

# The schemaVersion=1 compatibility layer must be bound to the richer dynamic
# groups, not maintained as an independent unchecked copy.
$legacy=$baseline.businessInvariants
if(@($legacy.counts.PSObject.Properties).Count-ne 23 -or @($legacy.sums.PSObject.Properties).Count-ne 10 -or @($legacy.time_bounds_utc.PSObject.Properties).Count-ne 5 -or @($legacy.status_distributions.PSObject.Properties).Count-ne 12 -or @($legacy.binary_nonempty_counts.PSObject.Properties).Count-ne 3){Fail 'Legacy business-invariant key set is incomplete.'}
foreach($p in $legacy.counts.PSObject.Properties){
    $dynamic=$baseline.counts.PSObject.Properties[$p.Name]
    if($dynamic){Assert-Equal "$($p.Value)" "$($dynamic.Value.total)" 'legacy count binding'}else{if([long]$p.Value-ne 0){Fail 'Missing legacy table must be represented as zero.'};$safe=ConvertTo-PgIdentifier $p.Name;Assert-Equal (Invoke-One "select case when to_regclass('public.$($p.Name)') is null then '0' else '1' end;") '0' 'missing future table'}
}
$sumBindings=[ordered]@{order_items_amount='order_items.amount';orders_total_amount='orders.total_amount';payments_fee_amount='payments.fee_amount';order_items_quantity='order_items.quantity';payments_payable_amount='payments.payable_amount';import_batches_total_rows='import_batches.total_rows';payments_submitted_amount='payments.submitted_amount';import_batches_failed_rows='import_batches.failed_rows';import_batches_success_rows='import_batches.success_rows';payment_items_applied_amount='payment_items.applied_amount'}
foreach($p in $legacy.sums.PSObject.Properties){$key=$sumBindings[$p.Name];$source=$baseline.numericSums.PSObject.Properties[$key];if(-not $source){$source=$baseline.integerSums.PSObject.Properties[$key]};if(-not $source){Fail 'Legacy sum lacks dynamic binding.'};$sum=if($null-eq$source.Value.sum){'0'}else{"$($source.Value.sum)"};Assert-Equal "$($p.Value)" $sum 'legacy sum binding'}
$statusBindings=[ordered]@{users='users.status';admins='admins.status';orders='orders.status';payments='payments.status';products='products.status';projects='projects.status';import_batches='import_batches.status';order_items_payment='order_items.payment_status';user_recovery_emails='user_recovery_emails.status';query_code_recovery_codes='query_code_recovery_codes.status';query_code_recovery_sessions='query_code_recovery_sessions.status';recovery_email_verification_codes='recovery_email_verification_codes.status'}
foreach($p in $legacy.status_distributions.PSObject.Properties){$dynamic=$baseline.statusDistributions.PSObject.Properties[$statusBindings[$p.Name]];if(-not $dynamic){Fail 'Legacy status lacks dynamic binding.'};$left=[ordered]@{};foreach($x in $p.Value.PSObject.Properties){$left[$x.Name]="$($x.Value)"};$right=[ordered]@{};foreach($x in $dynamic.Value.PSObject.Properties){$right[$x.Name]="$($x.Value)"};Assert-Equal (Get-CanonicalJson $left) (Get-CanonicalJson $right) 'legacy status binding'}
foreach($p in $legacy.time_bounds_utc.PSObject.Properties){$dynamic=$baseline.utcTimeBounds.PSObject.Properties["$($p.Name).created_at"];if(-not $dynamic){Fail 'Legacy time bound lacks dynamic binding.'};$actual=@($dynamic.Value.minUtc,$dynamic.Value.maxUtc);Assert-Equal ($actual|ConvertTo-Json -Compress) (@($p.Value)|ConvertTo-Json -Compress) 'legacy time binding'}
$binaryBindings=[ordered]@{payment_qr_codes_image_data='payment_qr_codes.image_data';payment_submissions_image_data='payment_submissions.image_data';user_recovery_emails_encrypted_email='user_recovery_emails.encrypted_email'}
foreach($p in $legacy.binary_nonempty_counts.PSObject.Properties){$dynamic=$baseline.binaryNonemptyCounts.PSObject.Properties[$binaryBindings[$p.Name]];$value=if($dynamic){"$($dynamic.Value.nonEmpty)"}else{'0'};Assert-Equal "$($p.Value)" $value 'legacy binary binding'}

$ownerResult=Invoke-One "select (select pg_get_userbyid(datdba) from pg_database where datname=current_database())||'|'||(select count(*) from pg_class c join pg_namespace n on n.oid=c.relnamespace where n.nspname='public' and c.relkind in('r','p','S','v','m') and pg_get_userbyid(c.relowner)<>'$ExpectedOwner')::text;"
Assert-Equal $ownerResult "$ExpectedOwner|0" 'owner'

Write-Output 'Snapshot restore baseline PASS'
Write-Output "  migrations : $($actualMigrations.Count)"
Write-Output "  counts     : $countChecked"
Write-Output "  statuses   : $statusChecked"
Write-Output "  binary     : $binaryChecked"
Write-Output "  numerics   : $numericChecked"
Write-Output "  integers   : $integerChecked"
Write-Output "  UTC bounds : $timeChecked"
Write-Output "  owner      : expected owner verified"
exit 0
