@echo off

REM Create the build directory if it doesn't exist
if not exist build (
    mkdir build
)

REM Build the Linux version
set "GOOS=linux"
set "GOARCH=amd64"
go build -ldflags="-s -w" -o build\convert_flac_to_opus convert_flac_to_opus.go
if errorlevel 1 (
    echo Failed to build Linux binary.
    exit /b %ERRORLEVEL%
)

REM Build the Windows version
set "GOOS=windows"
set "GOARCH=amd64"
go build -ldflags="-s -w" -o build\convert_flac_to_opus.exe convert_flac_to_opus.go
if errorlevel 1 (
    echo Failed to build Windows binary.
    exit /b %ERRORLEVEL%
)

echo Build completed successfully.
exit /b 0