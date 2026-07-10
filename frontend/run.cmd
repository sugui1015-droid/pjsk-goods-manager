@echo off
setlocal

cd /d "%~dp0"
pnpm dev --host 0.0.0.0
