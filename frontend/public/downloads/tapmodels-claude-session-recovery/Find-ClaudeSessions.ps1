[CmdletBinding()]
param(
  [string]$ClaudeHome = "",
  [int]$Limit = 20,
  [switch]$CommandsOnly,
  [switch]$Json
)

$ErrorActionPreference = "Stop"
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$python = Get-Command python -ErrorAction SilentlyContinue
if (-not $python) { $python = Get-Command py -ErrorAction SilentlyContinue }
if (-not $python) { throw "Python 3 is required. Install Python, then run this script again." }

$arguments = @((Join-Path $scriptDir "find_claude_sessions.py"), "--shell", "powershell", "--limit", $Limit)
if ($ClaudeHome) { $arguments += @("--claude-home", $ClaudeHome) }
if ($CommandsOnly) { $arguments += "--commands-only" }
if ($Json) { $arguments += "--json" }
& $python.Source @arguments
exit $LASTEXITCODE
