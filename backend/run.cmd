@echo off
setlocal

cd /d "%~dp0"
set "PATH=D:\go\bin;%PATH%"
set "GOCACHE=D:\pjsk\.cache\go-build"
set "GOPATH=D:\pjsk\.cache\gopath"
set "GOMODCACHE=D:\pjsk\.cache\gopath\pkg\mod"

go run .
