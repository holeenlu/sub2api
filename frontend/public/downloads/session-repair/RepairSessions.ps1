[CmdletBinding()]
param(
  [string]$CodexHome = "",
  [string]$Database = "",
  [string]$Rollback = "",
  [switch]$DryRun,
  [switch]$Apply,
  [switch]$ClientClosed,
  [switch]$Json
)

$ErrorActionPreference = "Stop"
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$python = Get-Command python -ErrorAction SilentlyContinue
if (-not $python) { $python = Get-Command py -ErrorAction SilentlyContinue }
if (-not $python) { throw "Python 3 is required. Install Python, then run this script again." }

$arguments = @((Join-Path $scriptDir "repair_sessions.py"))
if ($CodexHome) { $arguments += @("--codex-home", $CodexHome) }
if ($Database) { $arguments += @("--database", $Database) }
if ($Rollback) { $arguments += @("--rollback", $Rollback) }
if ($DryRun) { $arguments += "--dry-run" }
if ($Apply) { $arguments += "--apply" }
if ($ClientClosed) { $arguments += "--client-closed" }
if ($Json) { $arguments += "--json" }
& $python.Source @arguments
exit $LASTEXITCODE
