# Ensure the build directory exists
New-Item -ItemType Directory -Path "build" -Force | Out-Null

# Build for Linux (amd64)
$Env:GOOS = "linux"
$Env:GOARCH = "amd64"
& go build -ldflags "-s -w" -o "build/convert_flac_to_opus" "convert_flac_to_opus.go"

# Build for Windows (amd64)
$Env:GOOS = "windows"
$Env:GOARCH = "amd64"
& go build -ldflags "-s -w" -o "build/convert_flac_to_opus.exe" "convert_flac_to_opus.go"

# Clean up environment variables (optional)
Remove-Item Env:\GOOS
Remove-Item Env:\GOARCH