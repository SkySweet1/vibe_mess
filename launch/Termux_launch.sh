#!/bin/bash

echo "Установка мессенджера для Termux (Android)"

# 1. Обновление пакетов
echo "Обновление пакетов..."
pkg update -y && pkg upgrade -y

# 2. Установка Go и Git
echo "Установка Go и Git..."
pkg install -y golang git

# 3. Клонирование репозитория
if [ ! -d "vibe_mess" ]; then
    echo "Клонирование репозитория..."
    git clone https://github.com/SkySweet1/vibe_mess.git
else
    echo "Репозиторий уже есть, обновляем..."
    cd vibe_mess && git pull && cd ..
fi

# 4. Сборка клиента
cd vibe_mess/client
echo "🔨 Сборка клиента..."
go build -o messenger client.go

echo ""
echo "ГОТОВО!"
echo "Запуск: cd vibe_mess/client && ./messenger IP:2323"