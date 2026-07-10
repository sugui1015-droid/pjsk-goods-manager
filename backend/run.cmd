@echo off
setlocal

cd /d "%~dp0"
set "PATH=D:\go\bin;%PATH%"
set "GOCACHE=D:\pjsk\.cache\go-build"

go run .
