@echo off
rem dev: debug script
rem chain: frontend build -> copy dist to core/web/dist -> go build (bin/) -> npm run start
setlocal EnableExtensions
cd /d "%~dp0.."

echo [dev] building frontend...
cd frontend
call npm run build
if errorlevel 1 exit /b 1
cd ..

powershell -NoProfile -Command "Remove-Item -Recurse -Force core/web/dist -ErrorAction SilentlyContinue; Copy-Item -Recurse frontend/dist core/web/dist"

echo [dev] building core...
if not exist bin mkdir bin
cd core
go build -o ..\bin\ezharness-core.exe .
if errorlevel 1 exit /b 1
cd ..

echo [dev] starting desktop...
cd desktop
call npm run start
