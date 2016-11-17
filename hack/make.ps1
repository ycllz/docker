<#
.NOTES
    Author:  @jhowardmsft

    Summary: Windows native build script. This is similar to functionality provided
             by hack\make.sh, but uses native Windows PowerShell semantics. It does
             not support the full set of options provided by the Linux counterpart.
             For example:
             
             - You can't cross-build Linux docker binaries on Windows
             - Hashes aren't generated on binaries
             - 'Releasing' isn't supported.
             - Integration tests. This is because they currently cannot run inside a container. 
             
             It does however provided the minimum necessary to support local Windows 
             development and parts of Windows to Windows CI.

             Usage Examples (run from repo root): 
                "hack\make.ps1 -Binary" to build the binaries
                "hack\make.ps1 -Binary -DynamicSQLite" to build binaries with the daemon dynamically linked to sqlite3.dll
                "hack\make.ps1 -Client" to build just the client 64-bit binary
                "hack\make.ps1 -TestUnit" to run unit tests
                "hack\make.ps1 -Binary -TestUnit" to build the binaries and run unit tests

.PARAMETER Client
     Builds the client binaries.

.PARAMETER Daemon
     Builds the daemon binary.

.PARAMETER Binary
     Builds the client binaries and the daemon binary. A convenient shortcut to `make.ps1 -Client -Daemon`.

.PARAMETER Race
     Use -race in go build and go test.

.PARAMETER Noisy
     Use -v in go build.

.PARAMETER All
     Use -a in go build.

.PARAMETER NoOptimise
     Use -gcflags -N -l in go build to disable optimisation (can aide debugging).

.PARAMETER DynamicSQLite
     Determines whether SQLite is built in. If set, sqlite3.dll will be required to run the daemon.
     See Dockerfile.windows for instructions on how to generate this file.

.PARAMETER CommitSuffix
     Allows a custom string to be appended to the commit ID.

.PARAMETER TestUnit
     Runs unit tests.

#>


#$StaticSQLite = $True

param(
    [Parameter(Mandatory=$false)][switch]$Client,
    [Parameter(Mandatory=$false)][switch]$Daemon,
    [Parameter(Mandatory=$false)][switch]$Binary,
    [Parameter(Mandatory=$false)][switch]$Race,
    [Parameter(Mandatory=$false)][switch]$Noisy,
    [Parameter(Mandatory=$false)][switch]$All,
    [Parameter(Mandatory=$false)][switch]$NoOptimise,
    [Parameter(Mandatory=$false)][switch]$DynamicSQLite,
    [Parameter(Mandatory=$false)][string]$CommitSuffix="",
    [Parameter(Mandatory=$false)][switch]$TestUnit
)

$ErrorActionPreference = "Stop"

# Utility function to get the commit ID of the repository
Function Get-GitCommit() {
    if (-not (Test-Path ".\.git")) {
        # If we don't have a .git directory, but we do have the environment
        # variable DOCKER_GITCOMMIT set, that can override it.
        if ($env:DOCKER_GITCOMMIT.Length -eq 0) {
            Throw ".git directory missing and DOCKER_GITCOMMIT environment variable not specified."
        }
        Write-Host "INFO: Git commit assumed from DOCKER_GITCOMMIT environment variable"
        return $env:DOCKER_GITCOMMIT
    }
    $gitCommit=$(git rev-parse --short HEAD)
    if ($(git status --porcelain --untracked-files=no).Length -ne 0) {
        $gitCommit="$gitCommit-unsupported"
        Write-Host ""
        Write-Warning "This version is unsupported because there are uncommitted file(s)."
        Write-Warning "Either commit these changes, or add them to .gitignore."
        git status --porcelain --untracked-files=no | Write-Warning
        Write-Host ""
    }
    return $gitCommit
}

# Utility function to get get the current build version of docker
Function Get-DockerVersion() {
    if (-not (Test-Path ".\VERSION")) { Throw "VERSION file not found. Is this running from the root of a docker repository?" }
    $v=$(Get-Content ".\VERSION" -raw).ToString().Replace("`n","").Trim()
    return $v
}

# Utility function to determine if we are running in a container or not.
# In Windows, we get this through an environment variable set in `Dockerfile.Windows`
Function Check-InContainer() {
    if ($env:FROM_DOCKERFILE.Length -eq 0) {
        Write-Host ""
		Write-Warning "Not running in a container. The result might be an incorrect build."
        Write-Host ""
    }
}

try {
    $pushed=$false

    Write-Host -ForegroundColor Cyan "INFO: make.ps1 starting at $(Get-Date)"
    $root=$(pwd)
    if ($Binary) { $Client = $True; $Daemon = $True }
    if ($DynamicSQLite -and -not($Daemon)) { Throw "-DynamicSQLite cannot be used without -Daemon" }

    # Verify git is installed
    if ($(Get-Command git -ErrorAction SilentlyContinue) -eq $nil) { Throw "Git does not appear to be installed" }

    # Get the git commit. This will also verify if we are in a repo or not.
    $gitCommit=Get-GitCommit + "$CommitSuffix"
    if ($CommitSuffix -ne "") { $gitCommit += "-"+$CommitSuffix }

    # Get the version of docker (eg 1.14.0-dev)
    $dockerVersion=Get-DockerVersion

    # Give a warning if we are not running in a container
    Check-InContainer

    # Verify GOPATH is set
    if ($env:GOPATH.Length -eq 0) { Throw "Missing GOPATH environment variable. See https://golang.org/doc/code.html#GOPATH" }

    # Run autogen
    Write-Host "INFO: Invoking autogen..."
    $staticSQLiteString="SQLite is compiled into this binary."
    if (-not $StaticSQLite) {$staticSQLiteString = "This binary requires SQLite3.dll."}
    .\hack\make\.go-autogen.ps1 -CommitString $gitCommit -DockerVersion $dockerVersion -StaticSQLiteString $staticSQLiteString

    # Create the bundles directory
    if (-not (Test-Path ".\bundles")) { New-Item ".\bundles" -ItemType Directory | Out-Null }


    if ($Client -or $Daemon) {
        $buildTags="autogen"
        if ($Noisy) { $verboseParm=" -v" }
        if ($Race) { 
            Write-Warning "Using race detector"
            $raceParm=" -race" 
        }
        $allParam=""
        if ($All) { $allParm=" -a" }
        $optParam=""
        if ($NoOptimise) { $optParm=' -gcflags """ + "-N -l" + """' }
    }

    # Note -linkmode=internal required to be able to debug on Windows.
    # https://github.com/golang/go/issues/14319#issuecomment-189576638
    # Build the daemon binary if necessary
    if ($Daemon) {
        Write-Host "INFO: Building daemon..."
        if ($DynamicSQLite) { 
            Write-Host ""
            Write-Warning "dockerd.exe will require SQLite3.dll to run"
            Write-Host ""
        }
        $buildTags = "autogen daemon"
        pushd $root\cmd\dockerd
        $pushed=$True
        if ($DynamicSQLite) { $buildTags += " libsqlite3 sqlite_omit_load_extension" }
        $buildCommand = "go build" +$raceParm + $verboseParm + $allParm + $optParam + `
                        " -tags """ + $buildTags + """" + `
                        " -ldflags """ + "-linkmode=internal" + """ " + `
                        " -o $root\bundles\dockerd.exe"
        Invoke-Expression $buildCommand
        popd
        $pushed=$False
        if ($LASTEXITCODE -ne 0) { Throw "Daemon binary compile failed" }
   }

    # Build the client binaries if necessary
    if ($Client) {
        Write-Host "INFO: Building client..."
        $buildTags = "autogen"
        pushd $root\cmd\docker
        $pushed=$True
        $buildCommand = "go build" +$raceParm + $verboseParm + $allParm + $optParam + `
                        " -tags """ + $buildTags + """" + `
                        " -ldflags """ + "-linkmode=internal" + """ " + `
                        " -o $root\bundles\docker.exe"
        Invoke-Expression $buildCommand
        popd
        $pushed=$False
        if ($LASTEXITCODE -ne 0) { Throw "Client binary compile failed" }
    }

    if ($Daemon -or $Client) {
        Write-Host
        Write-Host -foregroundcolor green " ________   ____  __."
        Write-Host -foregroundcolor green " \_____  \ `|    `|/ _`|"
        Write-Host -foregroundcolor green " /   `|   \`|      `<"
        Write-Host -foregroundcolor green " /    `|    \    `|  \"
        Write-Host -foregroundcolor green " \_______  /____`|__ \"
        Write-Host -foregroundcolor green "         \/        \/"
        Write-Host
    }

    # Run unit tests
    if ($TestUnit) {
        Write-Host "INFO: Running unit tests..."
        $testPath="./..."
        $goListCommand = "go list -e -f '{{if ne .Name """ + '\"github.com/docker/docker\"' + """}}{{.ImportPath}}{{end}}' $testPath"
        $pkgList = $(Invoke-Expression $goListCommand)
        if ($LASTEXITCODE -ne 0) { Throw "go list for unit tests failed" }
        $pkgList = $pkgList | Select-String -Pattern "github.com/docker/docker"
        $pkgList = $pkgList | Select-String -NotMatch "github.com/docker/docker/vendor"
        $pkgList = $pkgList | Select-String -NotMatch "github.com/docker/docker/man"
        $pkgList = $pkgList | Select-String -NotMatch "github.com/docker/docker/integration-cli"
        $pkgList = $pkgList -replace "`r`n", " "
        $goTestCommand = "go test" + $raceParm + " -cover -ldflags -w -tags """ + "autogen daemon" + """ -a """ + "-test.timeout=10m" + """ $pkgList"
        Invoke-Expression $goTestCommand
        if ($LASTEXITCODE -ne 0) { Throw "Unit tests failed" }
    }


}
Catch [Exception] {
    Write-Host -ForegroundColor Red ("`nERROR: make.ps1 failed: '$_'`n")

    Write-Host -ForegroundColor red $_
    Write-Host
    Write-Host -ForegroundColor red  "___________      .__.__             .___"
    Write-Host -ForegroundColor red  "\_   _____/____  `|__`|  `|   ____   __`| _/"
    Write-Host -ForegroundColor red  " `|    __) \__  \ `|  `|  `| _/ __ \ / __ `| "
    Write-Host -ForegroundColor red  " `|     \   / __ \`|  `|  `|_\  ___// /_/ `| "
    Write-Host -ForegroundColor red  " \___  /  (____  /__`|____/\___  `>____ `| "
    Write-Host -ForegroundColor red  "     \/        \/             \/     \/ "
    Write-Host
}
Finally {
    if ($pushed) { popd }
    Write-Host -ForegroundColor Cyan "INFO: make.ps1 ended at at $(Get-Date)"
}
