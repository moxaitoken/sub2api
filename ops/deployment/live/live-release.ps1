[CmdletBinding()]
param(
    [ValidateSet('live')]
    [string]$Environment = 'live',
    [string]$Commit = 'HEAD',
    [string[]]$Components = @('main', 'supplier'),
    [Alias('Host')]
    [string]$TargetHost = 'root@47.236.9.72',
    [string]$ReleaseRoot = "$HOME\.codex\artifacts\sub2api-live-releases",
    [string]$Rollback,
    [switch]$Plan,
    [switch]$Execute,
    [switch]$AllowMigrations,
    [switch]$SkipTests
)

$ErrorActionPreference = 'Stop'
if ($Rollback -and ($Plan -or $Execute)) { throw 'Rollback cannot be combined with Plan or Execute.' }
if ($Plan -and $Execute) { throw 'Plan and Execute are mutually exclusive.' }
if (-not $Plan -and -not $Execute -and -not $Rollback) { $Plan = $true }

$python = Get-Command python -ErrorAction SilentlyContinue
if (-not $python) { throw 'Python 3 is required to run scripts/live-release.py.' }

$arguments = @(
    (Join-Path $PSScriptRoot 'live-release.py'),
    '--environment', $Environment,
    '--commit', $Commit,
    '--components', ($Components -join ','),
    '--host', $TargetHost,
    '--release-root', $ReleaseRoot
)
if ($Plan) { $arguments += '--plan' }
if ($Execute) { $arguments += '--execute' }
if ($AllowMigrations) { $arguments += '--allow-migrations' }
if ($Rollback) { $arguments += @('--rollback', $Rollback) }
if ($SkipTests) { $arguments += '--skip-tests' }

& $python.Source @arguments
exit $LASTEXITCODE
