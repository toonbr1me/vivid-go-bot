[CmdletBinding()]
param(
    [switch]$Integration,
    [switch]$Coverage,
    [switch]$Race
)

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$previousRequired = $env:REQUIRE_TEST_DATABASE
Push-Location -LiteralPath $projectRoot
try {
    if ($Integration) {
        if ([string]::IsNullOrWhiteSpace($env:TEST_DATABASE_URL)) {
            throw 'Set TEST_DATABASE_URL to a dedicated test database before using -Integration. DATABASE_URL is never used.'
        }
        $env:REQUIRE_TEST_DATABASE = 'true'
    }
    if ($Race) {
        $cgoEnabled = & go env CGO_ENABLED
        if ($LASTEXITCODE -ne 0 -or $cgoEnabled -ne '1') {
            throw '-Race requires CGO_ENABLED=1 and a supported C compiler. The regular test suite does not require CGO.'
        }
    }
    $testArgs = @('test', '-count=1')
    if ($Race) { $testArgs += '-race' }
    if ($Coverage) {
        $artifactDirectory = Join-Path $projectRoot '.codex-attachments'
        New-Item -ItemType Directory -Path $artifactDirectory -Force | Out-Null
        $coveragePath = Join-Path $artifactDirectory 'coverage.out'
        $testArgs += "-coverprofile=$coveragePath"
    }
    $testArgs += './...'
    & go @testArgs
    if ($LASTEXITCODE -ne 0) { throw 'Go tests failed.' }
    & go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'go vet failed.' }
    $javascriptTests = @('internal/bot/widgets/interface.test.cjs') + @(Get-ChildItem -LiteralPath (Join-Path $projectRoot 'internal/web/static') -Filter '*.test.cjs' | ForEach-Object { $_.FullName })
    & node --test @javascriptTests
    if ($LASTEXITCODE -ne 0) { throw 'Widget JavaScript tests failed.' }
    if ($Coverage) {
        & go tool cover "-html=$coveragePath" "-o=$(Join-Path $artifactDirectory 'coverage.html')"
        if ($LASTEXITCODE -ne 0) { throw 'Coverage report generation failed.' }
    }
} finally {
    $env:REQUIRE_TEST_DATABASE = $previousRequired
    Pop-Location
}
