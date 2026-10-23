@echo off
chcp 65001 >nul
cd /d "%~dp0"
echo.
echo   正在启动 MailDesk AI  ...
echo   浏览器访问 http://localhost:8080
echo   首次登录密钥 admin123 （登录后请到 设置-安全 修改）
echo.
bin\maildesk-windows-amd64.exe -addr :8080 -data .\data %*
echo.
echo 程序已退出。
pause
