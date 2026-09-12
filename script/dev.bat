@echo off
rem dev: vite 热更 + Electron 壳(dev URL) + core(壳拉起已构建 exe)
setlocal
cd /d "%~dp0.."

if exist ezharness-core.exe goto :vite

echo [dev] core exe 不存在，先全量构建(前端 -> core)...
cd frontend
call npm run build
if errorlevel 1 goto :err
cd ..
powershell -NoProfile -Command "Remove-Item -Recurse -Force core/dist -ErrorAction SilentlyContinue; Copy-Item -Recurse frontend/dist core/dist"
cd core
go build -o ..\ezharness-core.exe .
if errorlevel 1 goto :err
cd ..

:vite
start "ezharness vite" cmd /c "cd frontend && npm run dev"
timeout /t 3 /nobreak >nul

set EZHARNESS_DEV_URL=http://localhost:5173
cd desktop
npx electron .
goto :eof

:err
echo dev.bat failed
exit /b 1
