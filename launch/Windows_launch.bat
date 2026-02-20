@echo off
echo Установка мессенджера для Windows

:: Проверка Go
where go >nul 2>nul
if %errorlevel% neq 0 (
    echo Go не найден! Скачай и установи с https://golang.org/dl/
    echo После установки перезапусти этот скрипт
    pause
    exit /b
) else (
    echo Go найден: %go version%
)

:: Клонирование репозитория
if not exist "vibe_mess" (
    echo Клонирование репозитория...
    git clone https://github.com/SkySweet1/vibe_mess.git
    if %errorlevel% neq 0 (
        echo Git не найден! Скачай с https://git-scm.com/download/win
        pause
        exit /b
    )
) else (
    echo Репозиторий уже есть
)

:: Сборка
cd vibe_mess\client
echo 🔨 Сборка клиента...
go build -o messenger.exe client.go

echo.
echo Запуск: cd vibe_mess\client ^&^& messenger.exe IP:2323
pause