@echo off
cd /d %~dp0
set CGO_ENABLED=1
"C:\Program Files\Go\bin\go.exe" build -o bin\linkedin-agent.exe .\cmd\cli
"C:\Program Files\Go\bin\go.exe" build -o bin\linkedin-scheduler.exe .\cmd\scheduler
echo Build complete!
