@echo off
rem Builds Keep the Light. Needs Go 1.24 or newer.
rem After changing VERSION or the icon, run "go generate" first.
setlocal
set GOOS=windows
set GOARCH=amd64
go build -trimpath -ldflags "-s -w -H windowsgui" -o "Keep the Light.exe" . || exit /b 1
go build -trimpath -tags cli -ldflags "-s -w" -o keepthelight_cli.exe . || exit /b 1
set /p V=<VERSION
echo Built Keep the Light %V%: "Keep the Light.exe" and keepthelight_cli.exe
