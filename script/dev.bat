@echo off
rem dev: debug script
rem chain: frontend build -> copy dist to core/web/dist -> go build (release/dev/) -> npm run start
rem release/dev is the local dev folder: build output AND runtime data live there
rem 用法：script\dev.bat [--version 1.0.0]  （--version 覆盖本次构建的版本号，
rem      测版本检查用；只进构建产物，version.yaml 与工作区不动）
setlocal EnableExtensions
rem 打开 modeldump（每次模型请求全量输入的打印，进终端与 data/logs/）；正式版不设即关闭
set "EZ_MODEL_DUMP=1"
cd /d "%~dp0.."
set "ROOT=%cd%"

set "DEV_VERSION="
:parse
if "%~1"=="" goto parsed
if /i "%~1"=="--version" goto take_version
echo [dev] 未知参数: %~1
exit /b 1
:take_version
set "DEV_VERSION=%~2"
shift & shift
goto parse
:parsed

if not defined DEV_VERSION goto no_ver
echo [dev] 版本覆盖: v%DEV_VERSION% （仅本次构建产物，version.yaml 不动）
copy /y "%ROOT%\frontend\package.json" "%ROOT%\frontend\package.json.devbak" >nul
node -e "const fs=require('fs');const p='frontend/package.json';const j=JSON.parse(fs.readFileSync(p,'utf8'));j.version='v'+process.argv[1];fs.writeFileSync(p,JSON.stringify(j,null,2)+'\n')" %DEV_VERSION%
:no_ver

echo [dev] building frontend...
cd frontend
call npm run build
set "RC=%errorlevel%"
cd /d "%ROOT%"
if exist "%ROOT%\frontend\package.json.devbak" move /y "%ROOT%\frontend\package.json.devbak" "%ROOT%\frontend\package.json" >nul
if not "%RC%"=="0" exit /b %RC%

powershell -NoProfile -Command "Remove-Item -Recurse -Force core/web/dist -ErrorAction SilentlyContinue; Copy-Item -Recurse frontend/dist core/web/dist"

echo [dev] building core...
if not exist release\dev mkdir release\dev
cd core
go build -o ..\release\dev\ezharness-core.exe .
if errorlevel 1 exit /b 1
cd ..

echo [dev] starting desktop...
cd desktop
call npm run start
